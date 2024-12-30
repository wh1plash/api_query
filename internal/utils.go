package internal

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

func RowsToCSV(rows *sql.Rows, writer io.Writer) error {
	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	// Получаем заголовки столбцов
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %v", err)
	}
	if err := csvWriter.Write(columns); err != nil {
		return fmt.Errorf("failed to write headers: %v", err)
	}

	// Считываем строки
	values := make([]interface{}, len(columns))
	scanArgs := make([]interface{}, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return fmt.Errorf("failed to scan row: %v", err)
		}
		record := make([]string, len(columns))
		for i, v := range values {
			if v != nil {
				record[i] = fmt.Sprintf("%v", v)
			}
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write record: %v", err)
		}
	}
	return rows.Err()
}

func SaveToCSV(data io.Reader) error {
	// Создаем папку data, если она не существует
	if err := os.MkdirAll("data", os.ModePerm); err != nil {
		return fmt.Errorf("failed to create data directory: %v", err)
	}

	// Генерируем имя файла на основе времени
	fileName := time.Now().Format("20060102_150405") + ".csv"
	filePath := filepath.Join("data", fileName)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, data); err != nil {
		return fmt.Errorf("failed to write data to file: %v", err)
	}

	log.Printf("Data saved to %s", filePath)
	return nil
}
