package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "./data/collector.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Println("=== Tasks ===")
	rows, _ := db.Query("SELECT id, status FROM tasks WHERE status != 'queued'")
	for rows.Next() {
		var id, status string
		rows.Scan(&id, &status)
		fmt.Printf("%s | %s\n", id, status)
	}
	rows.Close()
}
