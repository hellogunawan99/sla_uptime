package main

import (
	"database/sql"
	"log"
	"os/exec"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// Koneksi SQLite (sama seperti sebelumnya)
	sqliteDB, err := sql.Open("sqlite3", "./ping_results.db")
	if err != nil {
		log.Fatalf("Gagal membuka database SQLite: %v", err)
	}
	defer sqliteDB.Close()

	// Membuat tabel ping_results (sama seperti sebelumnya)
	createTableSQL := `
    CREATE TABLE IF NOT EXISTS ping_results (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        ip_id INT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
        status INT
    );
    `
	_, err = sqliteDB.Exec(createTableSQL)
	if err != nil {
		log.Fatalf("Gagal membuat tabel ping_results: %v", err)
	}
	// fmt.Println("Tabel ping_results sudah siap.")

	// Koneksi MySQL (sama seperti sebelumnya)
	mysqlDB, err := sql.Open("mysql", "user:pass@tcp(localhost:3306)/db")
	if err != nil {
		log.Fatalf("Gagal membuka koneksi MySQL: %v", err)
	}
	defer mysqlDB.Close()

	// Ambil daftar IP dari tabel ip_monitor (sama seperti sebelumnya)
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

				status := ping(ipData.IP)
				_, err := sqliteDB.Exec("INSERT INTO ping_results (ip_id, status) VALUES (?, ?)", ipData.ID, status)
				if err != nil {
					// log.Printf("Gagal menyimpan hasil ping untuk IP %s: %v", ipData.IP, err)
				} else {
					// fmt.Printf("Ping ke %s: %s\n", ipData.IP, status)
				}
			}(ipData)
		}

		wg.Wait()
	}

	// Looping ping setiap 5 detik dengan batasan konkurensi
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pingWithConcurrency(ips, sqliteDB, 300) // Batasi 10 goroutine secara konkuren
		}
	}
}

// Fungsi ping dan isWindows() tetap sama seperti sebelumnya
func ping(ip string) string {
	var cmd *exec.Cmd
	if isWindows() {
		cmd = exec.Command("ping", "-n", "1", ip)
	} else {
		cmd = exec.Command("ping", "-c", "1", ip)
	}

	err := cmd.Run()
	if err != nil {
		return "0"
	}
	return "1"
}

func isWindows() bool {
	return exec.Command("cmd", "/c", "echo", "Windows").Run() == nil
}
