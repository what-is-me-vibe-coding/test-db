package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// saveToFile 将 Database 序列化为 JSON 并原子写入文件。
// 使用先写临时文件再 Rename 的策略保证原子性。
func saveToFile(path string, db *Database) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create catalog directory: %w", err)
	}

	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal catalog: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write catalog temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename catalog file: %w", err)
	}
	return nil
}

// loadFromFile 从 JSON 文件加载 Database。
func loadFromFile(path string) (*Database, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewDatabase(), nil
		}
		return nil, fmt.Errorf("read catalog file: %w", err)
	}
	if len(data) == 0 {
		return NewDatabase(), nil
	}

	var db Database
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("unmarshal catalog: %w", err)
	}
	if db.Tables == nil {
		db.Tables = make(map[string]*Table)
	}
	return &db, nil
}
