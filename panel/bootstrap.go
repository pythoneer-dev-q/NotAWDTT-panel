package panel

import (
	"log"
	"sync"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

var bootstrapOnce sync.Once
var bootstrapErr error

// BootstrapDB инициализирует MongoDB и seed данных до старта VPN-сервера.
func BootstrapDB() error {
	bootstrapOnce.Do(func() {
		// 1. Инициализируем MongoDB
		m, err := paneldb.GetDefaultMongo()
		if err == nil && m != nil {
			log.Println("[MONGO] Успешное подключение к MongoDB")
			_ = m.MigrateFromSQLite(panelDBPath)
		} else {
			log.Printf("[MONGO] Подключение к MongoDB: %v (продолжаем инициализацию)", err)
		}

		// 2. Инициализируем локальный слой SQLite для совместимости если доступен
		_ = initPanelDB()

		ensureLegacySettingsImported()
		ensureDefaultWdttData()
	})
	return bootstrapErr
}
