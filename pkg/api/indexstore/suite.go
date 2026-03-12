package indexstore

import "time"

// Suite represents a unique benchmark suite discovered during indexing.
type Suite struct {
	ID            uint   `gorm:"primaryKey"`
	SuiteHash     string `gorm:"uniqueIndex;not null"`
	DiscoveryPath string `gorm:"not null;index"`
	Name          string
	TestsTotal    int
	IndexedAt     time.Time
}
