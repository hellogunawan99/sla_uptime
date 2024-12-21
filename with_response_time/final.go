package main

import (
	"database/sql"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// Koneksi SQLite
	sqliteDB, err := sql.Open("sqlite3", "../ping_results.db")
	if err != nil {
		log.Fatalf("Gagal membuka database SQLite: %v", err)
	}
	defer sqliteDB.Close()

	// Membuat tabel ping_results
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS ping_results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ip_id INT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		status INT,
		response_time FLOAT
	);
	`
	_, err = sqliteDB.Exec(createTableSQL)
	if err != nil {
		log.Fatalf("Gagal membuat tabel ping_results: %v", err)
	}

	// Koneksi MySQL
	mysqlDB, err := sql.Open("mysql", "gunawan:CANcer99@tcp(localhost:3306)/sla_uptime")
	if err != nil {
		log.Fatalf("Gagal membuka koneksi MySQL: %v", err)
	}
	defer mysqlDB.Close()

	// Ambil daftar IP dari tabel ip_monitor
	rows, err := mysqlDB.Query("SELECT id, ip FROM ip_monitor")
	if err != nil {
		log.Fatalf("Gagal mengambil data dari ip_monitor: %v", err)
	}
	defer rows.Close()

	var ips []struct {
		ID int
		IP string
	}

	for rows.Next() {
		var id int
		var ip string
		if err := rows.Scan(&id, &ip); err != nil {
			log.Fatalf("Gagal membaca baris: %v", err)
		}
		ips = append(ips, struct {
			ID int
			IP string
		}{ID: id, IP: ip})
	}

	if len(ips) == 0 {
		log.Fatal("Tidak ada IP yang ditemukan")
	}

	// Fungsi ping dengan batasan konkurensi
	pingWithConcurrency := func(ips []struct {
		ID int
		IP string
	}, sqliteDB *sql.DB, maxConcurrency int) {
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, maxConcurrency)

		for _, ipData := range ips {
			wg.Add(1)
			semaphore <- struct{}{}

			go func(ipData struct {
				ID int
				IP string
			}) {
				defer wg.Done()
				defer func() { <-semaphore }()

				status, responseTime := ping(ipData.IP)
				_, err := sqliteDB.Exec("INSERT INTO ping_results (ip_id, status, response_time) VALUES (?, ?, ?)", ipData.ID, status, responseTime)
				if err != nil {
					// log.Printf("Gagal menyimpan hasil ping untuk IP %s: %v", ipData.IP, err)
				}
			}(ipData)
		}

		wg.Wait()
	}

	// Looping ping setiap 5 detik dengan batasan konkurensi
	for {
		start := time.Now()
		pingWithConcurrency(ips, sqliteDB, 300)
		elapsed := time.Since(start)

		if elapsed < 5*time.Second {
			time.Sleep(5*time.Second - elapsed)
		} else {
			// log.Printf("Ping cycle took longer than 5 seconds: %v", elapsed)
		}
	}
}

// Fungsi ping
func ping(ip string) (string, float64) {
	var cmd *exec.Cmd
	if isWindows() {
		cmd = exec.Command("ping", "-n", "1", "-w", "1000", ip) // Timeout 1 detik
	} else {
		cmd = exec.Command("ping", "-c", "1", "-W", "1", ip) // Timeout 1 detik
	}

	output, err := cmd.Output()
	if err != nil {
		// log.Printf("Ping failed for IP %s: %v", ip, err)
		return "0", 0.0
	}

	// Cari waktu respons dari output
	var responseTime float64
	outputStr := string(output)
	// log.Printf("Ping output for %s: %s", ip, outputStr) // Debug output ping

	if isWindows() {
		matches := regexp.MustCompile(`time[=<]\s*(\d+)ms`).FindStringSubmatch(outputStr)
		if len(matches) > 1 {
			responseTime, _ = strconv.ParseFloat(matches[1], 64)
		}
	} else {
		// Perbaikan regex untuk format Unix/Linux
		matches := regexp.MustCompile(`min/avg/max/stddev = [\d.]+/([\d.]+)/`).FindStringSubmatch(outputStr)
		if len(matches) > 1 {
			responseTime, err = strconv.ParseFloat(matches[1], 64)
			if err != nil {
				// log.Printf("Error parsing response time for %s: %v", ip, err)
				responseTime = 0.0
			}
		}
	}

	// log.Printf("Parsed Response Time for %s: %f ms", ip, responseTime) // Debug response time

	return "1", responseTime
}

func isWindows() bool {
	return exec.Command("cmd", "/c", "echo", "Windows").Run() == nil
}
