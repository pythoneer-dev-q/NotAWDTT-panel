package paneldb

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	envLoadedOnce sync.Once
)

func init() {
	LoadEnv()
}

// LoadEnv загружает переменные окружения из .env файлов.
// Проверяет по очереди:
// 1. Путь из переменной WDTT_ENV_FILE
// 2. Локальный .env в текущей директории
// 3. .env в директории конфигурации (по умолчанию /etc/wdtt/.env)
func LoadEnv() {
	envLoadedOnce.Do(func() {
		candidates := []string{}

		if custom := os.Getenv("WDTT_ENV_FILE"); custom != "" {
			candidates = append(candidates, custom)
		}

		candidates = append(candidates, ".env")

		if cfgDir := os.Getenv("WDTT_CONFIG_DIR"); cfgDir != "" {
			candidates = append(candidates, filepath.Join(cfgDir, ".env"))
		}
		candidates = append(candidates, "/etc/wdtt/.env")

		for _, path := range candidates {
			if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
				if err := LoadEnvFile(path); err == nil {
					log.Printf("[ENV] Загружена конфигурация из: %s", path)
					break
				}
			}
		}
	})
}

// LoadEnvFile читает .env файл и устанавливает значения в os.Setenv.
func LoadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Удаляем внешние кавычки ("..." или '...')
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		if key != "" {
			// Устанавливаем значение переменной
			_ = os.Setenv(key, val)
		}
	}
	return scanner.Err()
}
