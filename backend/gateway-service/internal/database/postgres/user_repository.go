package postgres

import (
	"context"
	"errors"
	"fmt"

	"gateway-service/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

func (ur *UserRepository) CreateUser(ctx context.Context, user domain.User) error {
	const query = `
		INSERT INTO users (
			id,
			yandex_id,
			created_at
		)
		VALUES ($1, $2, $3)
	`

	_, err := ur.db.Exec(
		ctx,
		query,
		user.ID,
		user.YandexID,
		user.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", mapUserDBError(err))
	}

	return nil
}

func (ur *UserRepository) GetUserByID(
	ctx context.Context,
	userID uuid.UUID,
) (domain.User, error) {
	const query = `
		SELECT
			id,
			yandex_id,
			created_at
		FROM users
		WHERE id = $1
	`

	var user domain.User

	err := ur.db.QueryRow(
		ctx,
		query,
		userID,
	).Scan(
		&user.ID,
		&user.YandexID,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrUserNotFound
		}

		return domain.User{}, fmt.Errorf(
			"get user by id: %w",
			mapUserDBError(err),
		)
	}

	return user, nil
}

func (ur *UserRepository) GetUserByYID(
	ctx context.Context,
	yID string,
) (domain.User, error) {
	const query = `
		SELECT
			id,
			yandex_id,
			created_at
		FROM users
		WHERE yandex_id = $1
	`

	var user domain.User

	err := ur.db.QueryRow(
		ctx,
		query,
		yID,
	).Scan(
		&user.ID,
		&user.YandexID,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrUserNotFound
		}

		return domain.User{}, fmt.Errorf(
			"get user by yandex id: %w",
			mapUserDBError(err),
		)
	}

	return user, nil
}

func (ur *UserRepository) DeleteUserByYID(
	ctx context.Context,
	yID string,
) error {
	const query = `
		DELETE FROM users
		WHERE yandex_id = $1
	`

	result, err := ur.db.Exec(
		ctx,
		query,
		yID,
	)
	if err != nil {
		return fmt.Errorf(
			"delete user by yandex id: %w",
			mapUserDBError(err),
		)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

func mapUserDBError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch pgErr.Code {
	case "23505":
		switch pgErr.ConstraintName {
		case "users_yandex_id_unique":
			return domain.ErrUserAlreadyExists
		}

	case "23514",
		"23502",
		"22001":
		return domain.ErrInvalidUser
	}

	return err
}
