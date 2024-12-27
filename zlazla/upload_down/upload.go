package main

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
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
	mysqlDB, err := sql.Open("mysql", "username:pass@tcp(localhost:3306)/sla_uptime")
	if err != nil {
		log.Fatalf("Gagal membuka koneksi MySQL: %v", err)
	}
	defer mysqlDB.Close()

	// Gunakan waktu UTC
	currentTime := time.Now().UTC()
	lastHour := currentTime.Truncate(time.Hour).Add(-0 * time.Hour) // Jam terakhir selesai (UTC)
	nextHour := lastHour.Add(time.Hour)                             // Akhir dari jam tersebut (UTC)

	fmt.Printf("Rentang waktu query: %s - %s (UTC)\n", lastHour, nextHour)

	// Query untuk mengambil data berdasarkan ip_id
	rows, err := sqliteDB.Query(`
        SELECT ip_id, status, response_time
        FROM ping_results
        WHERE timestamp BETWEEN ? AND ?
		AND status_id = 8
        ORDER BY ip_id, timestamp
    `, lastHour, nextHour)
	if err != nil {
		log.Fatalf("Gagal menjalankan query: %v", err)
	}
	defer rows.Close()

	// Map untuk menyimpan data response_time per ip_id
	ipData := make(map[int][]float64)
	statusCount := make(map[int][2]int) // Index 0: fail_count, Index 1: success_count

	for rows.Next() {
		var ipID int
		var status int
		var responseTime float64

		if err := rows.Scan(&ipID, &status, &responseTime); err != nil {
			log.Fatalf("Gagal membaca baris: %v", err)
		}

		// Tambahkan response_time ke map berdasarkan ip_id
		ipData[ipID] = append(ipData[ipID], responseTime)

		// Ambil array dari map, ubah, lalu simpan kembali
		counts := statusCount[ipID]
		if status == 1 {
			counts[1]++ // Tambahkan ke success_count
		} else {
			counts[0]++ // Tambahkan ke fail_count
		}
		statusCount[ipID] = counts // Tulis ulang ke map
	}

	// Simpan hasil ke MySQL
	for ipID, responseTimes := range ipData {
		successCount := statusCount[ipID][1]
		failCount := statusCount[ipID][0]

		totalPings := successCount + failCount
		uptimePercentage := 0.0
		if totalPings > 0 {
			uptimePercentage = (float64(successCount) / float64(totalPings)) * 100
		}

		// Hitung median response time
		medianResponseTime := calculateMedian(responseTimes)

		_, err := mysqlDB.Exec(`
            INSERT INTO uptime_summary (ip_id, timestamp, uptime_percentage, success_count, fail_count, response_time)
            VALUES (?, ?, ?, ?, ?, ?)
        `, ipID, lastHour, uptimePercentage, successCount, failCount, medianResponseTime)
		if err != nil {
			// log.Printf("Gagal menyimpan data uptime untuk ip_id %d: %v", ipID, err)
		} else {
			// fmt.Printf("Uptime untuk ip_id %d telah disimpan untuk rentang waktu %s - %s.\n", ipID, lastHour, nextHour)
		}
	}

	// Menghapus data yang lebih dari 6 jam di SQLite
	// threeHoursAgo := currentTime.Add(-6 * time.Hour)
	// _, err = sqliteDB.Exec(`DELETE FROM ping_results WHERE timestamp < ?`, threeHoursAgo)
	// if err != nil {
	// 	log.Printf("Gagal menghapus data lama: %v", err)
	// } else {
	// 	fmt.Println("Data lama yang lebih dari 3 jam telah dihapus dari SQLite.")
	// }
}

func calculateMedian(numbers []float64) float64 {
	if len(numbers) == 0 {
		return 0.0
	}

	sort.Float64s(numbers)

	mid := len(numbers) / 2
	if len(numbers)%2 == 0 {
		return (numbers[mid-1] + numbers[mid]) / 2
	}
	return numbers[mid]
}
