package store

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ErrNotFound is returned by Get and Update when no item has the given id.
// Callers identify it with errors.Is — no gorm import needed at the call
// site (e.g. the API layer maps it to 404).
var ErrNotFound = errors.New("item not found")

// DefaultDBPath is the default path for the wardrobe SQLite database.
const DefaultDBPath = "data/wardrobe.db"

// Item represents a wardrobe item per 04-data-schema.md.
type Item struct {
	ID              string    `gorm:"primaryKey"`
	Category        string    `gorm:"not null"`
	Subcategory     string    `gorm:"not null"`
	DominantColor   string    `gorm:"not null"`
	SecondaryColors []string  `gorm:"serializer:json"`
	Pattern         string    `gorm:"not null"`
	WarmthTier      string    `gorm:"not null"`
	Formality       string    `gorm:"not null"`
	PhotoPath       string    `gorm:"not null"`
	AddedDate       time.Time `gorm:"not null"`
	Notes           string
}

// Store wraps the GORM database connection.
type Store struct {
	db *gorm.DB
}

// Open creates the parent directory if needed, opens the SQLite database,
// and auto-migrates the schema.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	// GORM's default logger writes to os.Stdout, which would corrupt the
	// one-JSON-object-per-line stdout of the ingest CLI (ING-012). Keep the
	// diagnostics but send them to stderr, like the CLI's own log output.
	gormLogger := logger.New(
		log.New(os.Stderr, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold: 200 * time.Millisecond,
			LogLevel:      logger.Warn,
		},
	)

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: gormLogger})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&Item{}); err != nil {
		return nil, err
	}

	return &Store{db: db}, nil
}

// Insert adds a new wardrobe item to the store.
func (s *Store) Insert(item Item) error {
	return s.db.Create(&item).Error
}

// Get returns the item with the given id, or ErrNotFound.
func (s *Store) Get(id string) (Item, error) {
	var item Item
	err := s.db.First(&item, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	return item, nil
}

// Update persists the seven tagging fields and notes for the item with
// item.ID. id, added_date, and photo_path are left unchanged
// (04-data-schema.md "Write-path rules (interactive create/edit)"). If no
// item has that id it returns ErrNotFound and writes nothing. Enum
// validation stays in the tagging/API layer — not duplicated here.
func (s *Store) Update(item Item) error {
	existing, err := s.Get(item.ID)
	if err != nil {
		return err
	}
	existing.Category = item.Category
	existing.Subcategory = item.Subcategory
	existing.DominantColor = item.DominantColor
	existing.SecondaryColors = item.SecondaryColors
	existing.Pattern = item.Pattern
	existing.WarmthTier = item.WarmthTier
	existing.Formality = item.Formality
	existing.Notes = item.Notes
	return s.db.Save(&existing).Error
}

// List returns all wardrobe items from the store.
func (s *Store) List() ([]Item, error) {
	var items []Item
	err := s.db.Find(&items).Error
	return items, err
}

// Close closes the database connection.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}