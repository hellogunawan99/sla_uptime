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
		response_time FLOAT,
		status_id INT,
		reason_id INT
	);
	`
	_, err = sqliteDB.Exec(createTableSQL)
	if err != nil {
		log.Fatalf("Gagal membuat tabel ping_results: %v", err)
	}

	// Koneksi MySQL
	mysqlDB, err := sql.Open("mysql", "user:pass@tcp(localhost:3306)/sla_uptime")
	if err != nil {
		log.Fatalf("Gagal membuka koneksi MySQL: %v", err)
	}
	defer mysqlDB.Close()

	// Fungsi untuk mengambil data IP dari MySQL
	getIPsFromMySQL := func() []struct {
		ID       int
		IP       string
		StatusID int
		ReasonID int
	} {
		rows, err := mysqlDB.Query("SELECT id, ip, status_id, reason_id FROM ip_monitor")
		if err != nil {
			log.Printf("Gagal mengambil data dari ip_monitor: %v", err)
			return nil
		}
		defer rows.Close()

		var ips []struct {
			ID       int
			IP       string
			StatusID int
			ReasonID int
		}

		for rows.Next() {
			var id int
			var ip string
			var status_id int
			var reason_id int
			if err := rows.Scan(&id, &ip, &status_id, &reason_id); err != nil {
				log.Printf("Gagal membaca baris: %v", err)
				continue
			}
			ips = append(ips, struct {
				ID       int
				IP       string
				StatusID int
				ReasonID int
			}{ID: id, IP: ip, StatusID: status_id, ReasonID: reason_id})
		}

		return ips
	}

	// Fungsi ping dengan batasan konkurensi
	pingWithConcurrency := func(ips []struct {
		ID       int
		IP       string
		StatusID int
		ReasonID int
	}, sqliteDB *sql.DB, maxConcurrency int) {
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, maxConcurrency)

		for _, ipData := range ips {
			wg.Add(1)
			semaphore <- struct{}{}

			go func(ipData struct {
				ID       int
				IP       string
				StatusID int
				ReasonID int
			}) {
				defer wg.Done()
				defer func() { <-semaphore }()

				status, responseTime := ping(ipData.IP)
				_, err := sqliteDB.Exec("INSERT INTO ping_results (ip_id, status, response_time, status_id, reason_id) VALUES (?, ?, ?, ?, ?)",
					ipData.ID, status, responseTime, ipData.StatusID, ipData.ReasonID)
				if err != nil {
					log.Printf("Gagal menyimpan hasil ping untuk IP %s: %v", ipData.IP, err)
				}
			}(ipData)
		}

		wg.Wait()
	}

	// Timer untuk update data MySQL setiap 5 detik
	mysqlTicker := time.NewTicker(5 * time.Second)
	defer mysqlTicker.Stop()

	// Channel untuk menyimpan data IP terbaru
	ipChan := make(chan []struct {
		ID       int
		IP       string
		StatusID int
		ReasonID int
	})

	// Goroutine untuk update data MySQL
	go func() {
		for range mysqlTicker.C {
			ips := getIPsFromMySQL()
			if len(ips) > 0 {
				ipChan <- ips
			}
		}
	}()

	// Inisialisasi data IP pertama kali
	currentIPs := getIPsFromMySQL()
	if len(currentIPs) == 0 {
		log.Fatal("Tidak ada IP yang ditemukan")
	}

	// Loop utama
	pingTicker := time.NewTicker(5 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case newIPs := <-ipChan:
			currentIPs = newIPs
			log.Printf("Data IP diperbarui, jumlah IP: %d", len(currentIPs))
		case <-pingTicker.C:
			start := time.Now()
			pingWithConcurrency(currentIPs, sqliteDB, 300)
			elapsed := time.Since(start)
			if elapsed > 5*time.Second {
				log.Printf("Peringatan: Siklus ping memakan waktu lebih dari 5 detik: %v", elapsed)
			}
		}
	}
}

// Fungsi ping (tidak berubah)
func ping(ip string) (string, float64) {
	var cmd *exec.Cmd
	if isWindows() {
		cmd = exec.Command("ping", "-n", "1", "-w", "1000", ip)
	} else {
		cmd = exec.Command("ping", "-c", "1", "-W", "1", ip)
	}

	output, err := cmd.Output()
	if err != nil {
		return "0", 0.0
	}

	var responseTime float64
	outputStr := string(output)

	if isWindows() {
		matches := regexp.MustCompile(`time[=<]\s*(\d+)ms`).FindStringSubmatch(outputStr)
		if len(matches) > 1 {
			responseTime, _ = strconv.ParseFloat(matches[1], 64)
		}
	} else {
		matches := regexp.MustCompile(`min/avg/max/stddev = [\d.]+/([\d.]+)/`).FindStringSubmatch(outputStr)
		if len(matches) > 1 {
			responseTime, err = strconv.ParseFloat(matches[1], 64)
			if err != nil {
				responseTime = 0.0
			}
		}
	}

	return "1", responseTime
}

func isWindows() bool {
	return exec.Command("cmd", "/c", "echo", "Windows").Run() == nil
}
