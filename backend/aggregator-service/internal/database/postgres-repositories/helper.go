package postgres_repositories

import (
	"errors"

	"aggregator-service/internal/domain"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
)

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrRepositoryNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrRepositoryExists
		case "23503":
			return domain.ErrInvalidRepository
		}
	}

	return err
}

func mapGradeError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrGradeNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrGradeExists
		case "23503":
			return domain.ErrInvalidGrade
		}
	}

	return err
}

func mapLLMResponseError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrLLMResponseNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrLLMResponseExists
		case "23503":
			return domain.ErrInvalidRepository
		}
	}

	return err
}
