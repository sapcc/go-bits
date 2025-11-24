-- SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
-- SPDX-License-Identifier: Apache-2.0

-- SQL Backing Store Migration
--
-- This migration creates the required table for the SQL backing store.
-- The table will be created automatically by the backing store if skip_migration is false,
-- but you can also run this migration manually if you prefer to manage schema separately.

CREATE TABLE IF NOT EXISTS audit_events (
    id BIGSERIAL PRIMARY KEY,
    event_data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for efficient FIFO reads (oldest events first)
CREATE INDEX IF NOT EXISTS audit_events_created_at_id_idx
ON audit_events (created_at, id);

-- Optional: Add table comment for documentation
COMMENT ON TABLE audit_events IS 'Buffered audit events waiting to be sent to RabbitMQ';
COMMENT ON COLUMN audit_events.id IS 'Auto-incrementing primary key';
COMMENT ON COLUMN audit_events.event_data IS 'CADF event as JSON';
COMMENT ON COLUMN audit_events.created_at IS 'Timestamp when event was buffered';
