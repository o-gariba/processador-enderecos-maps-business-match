CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY,
    status VARCHAR(20) NOT NULL,
    result_path VARCHAR(255),
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
