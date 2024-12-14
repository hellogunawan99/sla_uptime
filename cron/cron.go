package main

import (
	"database/sql"
	"fmt"
	"log"
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

	// Koneksi MySQL
	mysqlDB, err := sql.Open("mysql", "user:pass@tcp(localhost:3306)/db")
	if err != nil {
		log.Fatalf("Gagal membuka koneksi MySQL: %v", err)
	}
	defer mysqlDB.Close()

	// Gunakan waktu UTC
	currentTime := time.Now().UTC()
	lastHour := currentTime.Truncate(time.Hour).Add(-1 * time.Hour) // Jam terakhir selesai (UTC)
	nextHour := lastHour.Add(time.Hour)                             // Akhir dari jam tersebut (UTC)

	fmt.Printf("Rentang waktu query: %s - %s (UTC)\n", lastHour, nextHour)

	// Query untuk menghitung uptime berdasarkan jam selesai
	rows, err := sqliteDB.Query(`
        SELECT ip_id,
               COUNT(CASE WHEN status = '1' THEN 1 END) AS success_count,
               COUNT(CASE WHEN status = '0' THEN 1 END) AS fail_count
        FROM ping_results
        WHERE timestamp BETWEEN ? AND ?
        GROUP BY ip_id
    `, lastHour, nextHour)
	if err != nil {
		log.Fatalf("Gagal menjalankan query: %v", err)
	}
	defer rows.Close()

	// Simpan hasil ke MySQL
	for rows.Next() {
		var ipID, successCount, failCount int
		if err := rows.Scan(&ipID, &successCount, &failCount); err != nil {
			log.Fatalf("Gagal membaca baris: %v", err)
		}

		totalPings := successCount + failCount
		uptimePercentage := 0.0
		if totalPings > 0 {
			uptimePercentage = (float64(successCount) / float64(totalPings)) * 100
		}

		_, err := mysqlDB.Exec(`
            INSERT INTO uptime_summary (ip_id, timestamp, uptime_percentage, success_count, fail_count)
            VALUES (?, ?, ?, ?, ?)
        `, ipID, lastHour, uptimePercentage, successCount, failCount)
		if err != nil {
			// log.Printf("Gagal menyimpan data uptime untuk ip_id %d: %v", ipID, err)
		} else {
			// fmt.Printf("Uptime untuk ip_id %d telah disimpan untuk rentang waktu %s - %s.\n", ipID, lastHour, nextHour)
		}
	}

	// Menghapus data yang lebih dari 3 jam di SQLite
	threeHoursAgo := currentTime.Add(-3 * time.Hour)
	_, err = sqliteDB.Exec(`DELETE FROM ping_results WHERE timestamp < ?`, threeHoursAgo)
	if err != nil {
		// log.Printf("Gagal menghapus data lama: %v", err)
	} else {
		// fmt.Println("Data lama yang lebih dari 3 jam telah dihapus dari SQLite.")
	}
}
