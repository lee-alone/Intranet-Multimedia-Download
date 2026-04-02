package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "./release/collector_1.0.0_windows_amd64/data/collector.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Println("=== Tasks ===")
	rows, err := db.Query("SELECT id, status, progress, file_path, error_message FROM tasks ORDER BY created_at DESC LIMIT 5")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, status string
		var progress float64
		var filePath, errorMessage sql.NullString
		rows.Scan(&id, &status, &progress, &filePath, &errorMessage)
		fmt.Printf("ID: %s\n", id)
		fmt.Printf("  Status: %s\n", status)
		fmt.Printf("  Progress: %.1f%%\n", progress)
		if filePath.Valid {
			fmt.Printf("  FilePath: %s\n", filePath.String)
		} else {
			fmt.Printf("  FilePath: NULL\n")
		}
		if errorMessage.Valid && errorMessage.String != "" {
			fmt.Printf("  Error: %s\n", errorMessage.String)
		}
		fmt.Println()
	}
}
