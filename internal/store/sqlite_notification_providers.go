package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// ── NotificationProviders ─────────────────────────────────────────────────────

func (s *sqliteStore) SaveNotificationProvider(ctx context.Context, p *model.NotificationProvider) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_providers (name, type, config, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.Name, p.Type, string(p.Config), boolToInt(p.Active),
		p.CreatedAt.UTC().Format(time.RFC3339),
		p.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save notification provider: %w", err)
	}
	id, _ := res.LastInsertId()
	p.ID = id
	return nil
}

func (s *sqliteStore) GetNotificationProvider(ctx context.Context, id int64) (*model.NotificationProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, config, active, created_at, updated_at
		 FROM notification_providers WHERE id = ?`, id)
	p, err := scanNotificationProvider(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get notification provider: %w", sql.ErrNoRows)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get notification provider: %w", err)
	}
	return p, nil
}

func (s *sqliteStore) ListNotificationProviders(ctx context.Context) ([]*model.NotificationProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, type, config, active, created_at, updated_at
		 FROM notification_providers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list notification providers: %w", err)
	}
	defer rows.Close()

	var result []*model.NotificationProvider
	for rows.Next() {
		p, err := scanNotificationProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list notification providers: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *sqliteStore) UpdateNotificationProvider(ctx context.Context, p *model.NotificationProvider) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_providers SET name=?, type=?, config=?, active=?, updated_at=? WHERE id=?`,
		p.Name, p.Type, string(p.Config), boolToInt(p.Active),
		p.UpdatedAt.UTC().Format(time.RFC3339), p.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update notification provider: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteNotificationProvider(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_providers WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("store: delete notification provider: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetNotificationProviderActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_providers SET active=?, updated_at=datetime('now') WHERE id=?`,
		boolToInt(active), id,
	)
	if err != nil {
		return fmt.Errorf("store: set notification provider active: %w", err)
	}
	return nil
}

type notifProviderScanner interface {
	Scan(dest ...any) error
}

func scanNotificationProvider(s notifProviderScanner) (*model.NotificationProvider, error) {
	var p model.NotificationProvider
	var activeInt int
	var configStr, createdAt, updatedAt string
	if err := s.Scan(&p.ID, &p.Name, &p.Type, &configStr, &activeInt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	p.Config = []byte(configStr)
	p.Active = activeInt != 0
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &p, nil
}

// ── NotificationChannels ──────────────────────────────────────────────────────

func (s *sqliteStore) SaveNotificationChannel(ctx context.Context, c *model.NotificationChannel) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_channels (name, provider_id, message_template, config, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.ProviderID, c.MessageTemplate, string(c.Config), boolToInt(c.Active),
		c.CreatedAt.UTC().Format(time.RFC3339),
		c.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save notification channel: %w", err)
	}
	id, _ := res.LastInsertId()
	c.ID = id
	return nil
}

func (s *sqliteStore) GetNotificationChannel(ctx context.Context, id int64) (*model.NotificationChannel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, provider_id, message_template, config, active, created_at, updated_at
		 FROM notification_channels WHERE id = ?`, id)
	c, err := scanNotificationChannel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get notification channel: %w", sql.ErrNoRows)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get notification channel: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) ListNotificationChannels(ctx context.Context, providerID *int64) ([]*model.NotificationChannel, error) {
	var rows *sql.Rows
	var err error
	if providerID != nil {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, provider_id, message_template, config, active, created_at, updated_at
			 FROM notification_channels WHERE provider_id=? ORDER BY id`, *providerID)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, provider_id, message_template, config, active, created_at, updated_at
			 FROM notification_channels ORDER BY id`)
	}
	if err != nil {
		return nil, fmt.Errorf("store: list notification channels: %w", err)
	}
	defer rows.Close()

	var result []*model.NotificationChannel
	for rows.Next() {
		c, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list notification channels: %w", err)
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *sqliteStore) UpdateNotificationChannel(ctx context.Context, c *model.NotificationChannel) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_channels SET name=?, provider_id=?, message_template=?, config=?, active=?, updated_at=? WHERE id=?`,
		c.Name, c.ProviderID, c.MessageTemplate, string(c.Config), boolToInt(c.Active),
		c.UpdatedAt.UTC().Format(time.RFC3339), c.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update notification channel: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteNotificationChannel(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("store: delete notification channel: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetNotificationChannelActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_channels SET active=?, updated_at=datetime('now') WHERE id=?`,
		boolToInt(active), id,
	)
	if err != nil {
		return fmt.Errorf("store: set notification channel active: %w", err)
	}
	return nil
}

type notifChannelScanner interface {
	Scan(dest ...any) error
}

func scanNotificationChannel(s notifChannelScanner) (*model.NotificationChannel, error) {
	var c model.NotificationChannel
	var activeInt int
	var configStr, createdAt, updatedAt string
	if err := s.Scan(&c.ID, &c.Name, &c.ProviderID, &c.MessageTemplate, &configStr, &activeInt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	c.Config = []byte(configStr)
	c.Active = activeInt != 0
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &c, nil
}
