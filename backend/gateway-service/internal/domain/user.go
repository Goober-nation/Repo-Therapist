package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID        uuid.UUID
	YandexID  string
	CreatedAt time.Time
}
