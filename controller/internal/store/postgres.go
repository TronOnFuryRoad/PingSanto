package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store backed by PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects to PostgreSQL using the supplied connection string.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Verify connection on startup.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresStore{pool: pool}, nil
}

// Close releases database resources.
func (p *PostgresStore) Close() {
	p.pool.Close()
}

func (p *PostgresStore) FetchUpgradePlan(ctx context.Context, agentID string, channel string) (UpgradePlanResponse, string, error) {
	if plan, etag, err := p.fetchPlanRecord(ctx, agentID); err == nil {
		return plan, etag, nil
	} else if err != nil && !errors.Is(err, ErrPlanNotFound) {
		return UpgradePlanResponse{}, "", err
	}

	if key := channelPlanKey(channel); key != "" {
		if plan, etag, err := p.fetchPlanRecord(ctx, key); err == nil {
			return plan, etag, nil
		} else if err != nil && !errors.Is(err, ErrPlanNotFound) {
			return UpgradePlanResponse{}, "", err
		}
	}

	return UpgradePlanResponse{}, "", ErrPlanNotFound
}

func (p *PostgresStore) fetchPlanRecord(ctx context.Context, key string) (UpgradePlanResponse, string, error) {
	const query = `
SELECT agent_id, channel, version, artifact_url, artifact_sha256,
       artifact_signature_url, force_apply, schedule_earliest, schedule_latest,
       paused, notes, etag, updated_at
  FROM agent_upgrade_plans
 WHERE agent_id = $1;
`
	row := p.pool.QueryRow(ctx, query, key)
	var plan UpgradePlanResponse
	var artifactURL, artifactSHA, signatureURL, etag, notes string
	var scheduleEarliest, scheduleLatest *time.Time
	var updatedAt time.Time
	var forceApply, paused bool
	var channelValue, version string
	if err := row.Scan(&plan.AgentID, &channelValue, &version, &artifactURL, &artifactSHA, &signatureURL,
		&forceApply, &scheduleEarliest, &scheduleLatest, &paused, &notes, &etag, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpgradePlanResponse{}, "", ErrPlanNotFound
		}
		return UpgradePlanResponse{}, "", err
	}

	plan.Channel = channelValue
	plan.GeneratedAt = updatedAt.UTC()
	plan.Artifact = Artifact{
		Version:      version,
		URL:          artifactURL,
		SHA256:       artifactSHA,
		SignatureURL: signatureURL,
		ForceApply:   forceApply,
	}
	plan.Schedule = Schedule{Earliest: scheduleEarliest, Latest: scheduleLatest}
	plan.Paused = paused
	plan.Notes = notes
	return plan, etag, nil
}

func (p *PostgresStore) RecordUpgradeReport(ctx context.Context, report UpgradeReport) error {
	const insert = `
INSERT INTO agent_upgrade_history (
    agent_id, channel, target_version, previous_version, status,
    message, details, started_at, completed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9);
`
	var detailsJSON any
	if report.Details != nil {
		b, err := json.Marshal(report.Details)
		if err != nil {
			return err
		}
		detailsJSON = b
	}
	_, err := p.pool.Exec(ctx, insert,
		report.AgentID,
		report.Channel,
		report.CurrentVersion,
		nullString(report.PreviousVersion),
		report.Status,
		nullString(report.Message),
		detailsJSON,
		report.StartedAt,
		report.CompletedAt,
	)
	return err
}

func (p *PostgresStore) UpsertUpgradePlan(ctx context.Context, input PlanInput) (UpgradePlanResponse, string, error) {
	if strings.TrimSpace(input.Version) == "" {
		return UpgradePlanResponse{}, "", errors.New("version required")
	}
	channel := defaultString(input.Channel, "stable")
	agentKey := strings.TrimSpace(input.AgentID)
	if agentKey == "" {
		agentKey = channelPlanKey(channel)
	}
	plan := UpgradePlanResponse{
		AgentID:     agentKey,
		GeneratedAt: time.Now().UTC(),
		Channel:     channel,
		Artifact: Artifact{
			Version:      input.Version,
			URL:          input.ArtifactURL,
			SHA256:       input.ArtifactSHA256,
			SignatureURL: input.SignatureURL,
			ForceApply:   input.ForceApply,
		},
		Schedule: Schedule{
			Earliest: input.ScheduleEarliest,
			Latest:   input.ScheduleLatest,
		},
		Paused: input.Paused,
		Notes:  input.Notes,
	}
	etag := computeETag(plan)

	const upsert = `
INSERT INTO agent_upgrade_plans (
    agent_id, channel, version, artifact_url, artifact_sha256,
    artifact_signature_url, force_apply, schedule_earliest, schedule_latest,
    paused, notes, etag, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
ON CONFLICT (agent_id) DO UPDATE SET
    channel = EXCLUDED.channel,
    version = EXCLUDED.version,
    artifact_url = EXCLUDED.artifact_url,
    artifact_sha256 = EXCLUDED.artifact_sha256,
    artifact_signature_url = EXCLUDED.artifact_signature_url,
    force_apply = EXCLUDED.force_apply,
    schedule_earliest = EXCLUDED.schedule_earliest,
    schedule_latest = EXCLUDED.schedule_latest,
    paused = EXCLUDED.paused,
    notes = EXCLUDED.notes,
    etag = EXCLUDED.etag,
    updated_at = NOW();
`
	_, err := p.pool.Exec(ctx, upsert,
		plan.AgentID,
		plan.Channel,
		plan.Artifact.Version,
		plan.Artifact.URL,
		plan.Artifact.SHA256,
		plan.Artifact.SignatureURL,
		plan.Artifact.ForceApply,
		plan.Schedule.Earliest,
		plan.Schedule.Latest,
		plan.Paused,
		nullString(plan.Notes),
		etag,
	)
	if err != nil {
		return UpgradePlanResponse{}, "", err
	}
	return plan, etag, nil
}

func (p *PostgresStore) ListUpgradeHistory(ctx context.Context, agentID string, limit int) ([]UpgradeReport, error) {
	if limit <= 0 {
		limit = 50
	}
	const query = `
SELECT agent_id, channel, target_version, previous_version, status,
       message, details, started_at, completed_at
  FROM agent_upgrade_history
 WHERE agent_id = $1
 ORDER BY completed_at DESC
 LIMIT $2;
`
	rows, err := p.pool.Query(ctx, query, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []UpgradeReport
	for rows.Next() {
		var r UpgradeReport
		var targetVersion string
		var prevVersion sql.NullString
		var message sql.NullString
		var detailsBytes []byte
		if err := rows.Scan(&r.AgentID, &r.Channel, &targetVersion, &prevVersion, &r.Status, &message, &detailsBytes, &r.StartedAt, &r.CompletedAt); err != nil {
			return nil, err
		}
		r.CurrentVersion = targetVersion
		if prevVersion.Valid {
			r.PreviousVersion = prevVersion.String
		}
		if message.Valid {
			r.Message = message.String
		}
		if len(detailsBytes) > 0 {
			var details map[string]any
			if err := json.Unmarshal(detailsBytes, &details); err == nil {
				r.Details = details
			}
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

func nullString(val string) any {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	return val
}

func (p *PostgresStore) GetNotificationSettings(ctx context.Context) (NotificationSettings, error) {
	const query = `SELECT notify_on_publish, updated_at FROM controller_settings WHERE id = TRUE`
	row := p.pool.QueryRow(ctx, query)
	var settings NotificationSettings
	if err := row.Scan(&settings.NotifyOnPublish, &settings.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// default to true if table not initialised
			return NotificationSettings{NotifyOnPublish: true, UpdatedAt: time.Now().UTC()}, nil
		}
		return NotificationSettings{}, err
	}
	return settings, nil
}

func (p *PostgresStore) UpdateNotificationSettings(ctx context.Context, notify bool) (NotificationSettings, error) {
	const upsert = `
INSERT INTO controller_settings (id, notify_on_publish, updated_at)
VALUES (TRUE, $1, NOW())
ON CONFLICT (id) DO UPDATE SET
    notify_on_publish = EXCLUDED.notify_on_publish,
    updated_at = NOW()
RETURNING notify_on_publish, updated_at;
`
	row := p.pool.QueryRow(ctx, upsert, notify)
	var settings NotificationSettings
	if err := row.Scan(&settings.NotifyOnPublish, &settings.UpdatedAt); err != nil {
		return NotificationSettings{}, err
	}
	return settings, nil
}
