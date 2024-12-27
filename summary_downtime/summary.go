package main

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// Koneksi ke MySQL dengan timeout dan parameter koneksi yang lebih baik
	dsn := "user:pass@tcp(localhost:3306)/sla_uptime?parseTime=true&loc=Local&timeout=30s&writeTimeout=30s&readTimeout=30s"
	mysqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Gagal membuka koneksi MySQL: %v", err)
	}
	defer mysqlDB.Close()

	// Test koneksi
	if err := mysqlDB.Ping(); err != nil {
		log.Fatalf("Gagal melakukan ping ke database: %v", err)
	}

	// Set connection pool parameters
	mysqlDB.SetMaxOpenConns(25)
	mysqlDB.SetMaxIdleConns(25)
	mysqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Menggunakan waktu lokal
	currentTime := time.Now()
	lastHour := currentTime.Truncate(time.Hour).Add(-1 * time.Hour)
	nextHour := lastHour.Add(time.Hour)

	fmt.Printf("Rentang waktu query: %s - %s\n", lastHour.Format(time.RFC3339), nextHour.Format(time.RFC3339))

	// Gunakan prepared statement untuk query
	stmt, err := mysqlDB.Prepare(`
        SELECT ip_id, status, response_time
        FROM ping_results
        WHERE timestamp BETWEEN ? AND ?
          AND status_id = 8
          AND reason_id != 16
        ORDER BY ip_id, timestamp
    `)
	if err != nil {
		log.Fatalf("Gagal mempersiapkan statement: %v", err)
	}
	defer stmt.Close()

	rows, err := stmt.Query(lastHour, nextHour)
	if err != nil {
		log.Fatalf("Gagal menjalankan query: %v", err)
	}
	defer rows.Close()

	ipData := make(map[int][]float64)
	statusCount := make(map[int][2]int)

	// Batched insert preparation
	insertStmt, err := mysqlDB.Prepare(`
        INSERT INTO summary_downtime (ip_id, timestamp, success_count, fail_count, response_time)
        VALUES (?, ?, ?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE
            uptime_percentage = VALUES(uptime_percentage),
            success_count = VALUES(success_count),
            fail_count = VALUES(fail_count),
            response_time = VALUES(response_time)
    `)
	if err != nil {
		log.Fatalf("Gagal mempersiapkan insert statement: %v", err)
	}
	defer insertStmt.Close()

	for rows.Next() {
		var ipID int
		var status int
		var responseTime float64

		if err := rows.Scan(&ipID, &status, &responseTime); err != nil {
			log.Printf("Warning: Gagal membaca baris: %v", err)
			continue
		}

		ipData[ipID] = append(ipData[ipID], responseTime)
		counts := statusCount[ipID]
		if status == 1 {
			counts[1]++
		} else {
			counts[0]++
		}
		statusCount[ipID] = counts
	}

	if err = rows.Err(); err != nil {
		log.Printf("Error setelah iterasi rows: %v", err)
	}

	// Transaction for batch insert
	tx, err := mysqlDB.Begin()
	if err != nil {
		log.Fatalf("Gagal memulai transaksi: %v", err)
	}

	for ipID, responseTimes := range ipData {
		successCount := statusCount[ipID][1]
		failCount := statusCount[ipID][0]

		totalPings := successCount + failCount
		var uptimePercentage float64
		if totalPings > 0 {
			uptimePercentage = (float64(successCount) / float64(totalPings)) * 100
		}

		medianResponseTime := calculateMedian(responseTimes)

		_, err := tx.Stmt(insertStmt).Exec(
			ipID,
			lastHour,
			uptimePercentage,
			successCount,
			failCount,
			medianResponseTime,
		)
		if err != nil {
			log.Printf("Gagal menyimpan data untuk ip_id %d: %v", ipID, err)
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				log.Printf("Gagal rollback: %v", rollbackErr)
			}
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Gagal commit transaksi: %v", err)
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.Printf("Gagal rollback: %v", rollbackErr)
		}
		return
	}
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
