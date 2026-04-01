package main

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "F:/校园资源采集系统/release/collector_1.0.0_windows_amd64/data/collector.db")
	if err != nil {
		fmt.Printf("Open DB error: %v\n", err)
		return
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, title, file_path, status FROM tasks`)
	if err != nil {
		fmt.Printf("Query error: %v\n", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, status string
		var title, filePath sql.NullString
		err := rows.Scan(&id, &title, &filePath, &status)
		if err != nil {
			fmt.Printf("Scan error: %v\n", err)
			continue
		}
		fmt.Printf("ID: %s\n", id)
		fmt.Printf("  Status: %s\n", status)
		if title.Valid {
			fmt.Printf("  Title: %s\n", title.String)
			fmt.Printf("  Title (bytes): %v\n", []byte(title.String))
		} else {
			fmt.Printf("  Title: <null>\n")
		}
		if filePath.Valid {
			fmt.Printf("  FilePath: %s\n", filePath.String)
			fmt.Printf("  FilePath (bytes): %v\n", []byte(filePath.String))
		} else {
			fmt.Printf("  FilePath: <null>\n")
		}
		fmt.Println("---")
	}
}
