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
)

func main() {
	// Koneksi MySQL untuk data monitoring
	db, err := sql.Open("mysql", "user:pass@tcp(localhost:3306)/sla_uptime")
	if err != nil {
		// log.Fatalf("Gagal membuka koneksi MySQL: %v", err)
	}
	defer db.Close()

	// Membuat tabel ping_results jika belum ada
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS ping_results (
		id INT AUTO_INCREMENT PRIMARY KEY,
		ip_id INT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		status VARCHAR(1),
		response_time FLOAT,
		status_id INT,
		reason_id INT,
		INDEX idx_ip_timestamp (ip_id, timestamp)
	);
	`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		// log.Fatalf("Gagal membuat tabel ping_results: %v", err)
	}

	// Fungsi untuk mengambil data IP
	getIPsFromMySQL := func() []struct {
		ID       int
		IP       string
		StatusID int
		ReasonID int
	} {
		rows, err := db.Query("SELECT id, ip, status_id, reason_id FROM ip_monitor")
		if err != nil {
			// log.Printf("Gagal mengambil data dari ip_monitor: %v", err)
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
			var ip struct {
				ID       int
				IP       string
				StatusID int
				ReasonID int
			}
			if err := rows.Scan(&ip.ID, &ip.IP, &ip.StatusID, &ip.ReasonID); err != nil {
				// log.Printf("Gagal membaca baris: %v", err)
				continue
			}
			ips = append(ips, ip)
		}

		return ips
	}

	// Fungsi ping dengan batasan konkurensi
	pingWithConcurrency := func(ips []struct {
		ID       int
		IP       string
		StatusID int
		ReasonID int
	}, db *sql.DB, maxConcurrency int) {
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, maxConcurrency)

		// Prepare statement untuk insert
		stmt, err := db.Prepare(`
			INSERT INTO ping_results 
			(ip_id, status, response_time, status_id, reason_id) 
			VALUES (?, ?, ?, ?, ?)
		`)
		if err != nil {
			// log.Printf("Gagal mempersiapkan statement: %v", err)
			return
		}
		defer stmt.Close()

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
				_, err := stmt.Exec(
					ipData.ID,
					status,
					responseTime,
					ipData.StatusID,
					ipData.ReasonID,
				)
				if err != nil {
					// log.Printf("Gagal menyimpan hasil ping untuk IP %s: %v", ipData.IP, err)
				}
			}(ipData)
		}

		wg.Wait()
	}

	// Timer untuk update data setiap 5 detik
	updateTicker := time.NewTicker(5 * time.Second)
	defer updateTicker.Stop()

	// Channel untuk menyimpan data IP terbaru
	ipChan := make(chan []struct {
		ID       int
		IP       string
		StatusID int
		ReasonID int
	})

	// Goroutine untuk update data
	go func() {
		for range updateTicker.C {
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
			// log.Printf("Data IP diperbarui, jumlah IP: %d", len(currentIPs))
		case <-pingTicker.C:
			start := time.Now()
			pingWithConcurrency(currentIPs, db, 300)
			elapsed := time.Since(start)
			if elapsed > 5*time.Second {
				// log.Printf("Peringatan: Siklus ping memakan waktu lebih dari 5 detik: %v", elapsed)
			}
		}
	}
}

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
