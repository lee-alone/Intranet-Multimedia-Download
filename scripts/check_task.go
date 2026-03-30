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

	var status string
	err = db.QueryRow("SELECT status FROM tasks WHERE id = ?", "58f3e942-86c9-4a75-916f-f5f2b951e9a6").Scan(&status)
	if err != nil {
		fmt.Printf("Not found: %v\n", err)
	} else {
		fmt.Printf("Status: %s\n", status)
	}
}
