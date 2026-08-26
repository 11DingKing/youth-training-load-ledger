package migrate

type Migration struct {
	Version int
	Name    string
	SQL     string
}

var All = []Migration{
	{
		Version: 1,
		Name:    "identity_and_profiles",
		SQL: `
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    display_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('student','guardian','coach','health_advisor')),
    password_hash TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX sessions_user_active_idx ON sessions(user_id, revoked_at, expires_at);
CREATE TABLE athletes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    student_user_id INTEGER NOT NULL UNIQUE REFERENCES users(id),
    guardian_user_id INTEGER NOT NULL REFERENCES users(id),
    coach_user_id INTEGER REFERENCES users(id),
    advisor_user_id INTEGER REFERENCES users(id),
    birth_date TIMESTAMP NOT NULL,
    timezone TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft','awaiting_consent','active','paused','closed')),
    version INTEGER NOT NULL DEFAULT 1,
    paused_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CHECK (student_user_id <> guardian_user_id)
);
CREATE INDEX athletes_guardian_idx ON athletes(guardian_user_id, status);
CREATE INDEX athletes_staff_idx ON athletes(coach_user_id, advisor_user_id, status);
CREATE TABLE guardian_consents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    guardian_id INTEGER NOT NULL REFERENCES users(id),
    status TEXT NOT NULL CHECK (status IN ('pending','granted','withdrawn','expired')),
    terms_version TEXT NOT NULL,
    effective_at TIMESTAMP,
    expires_at TIMESTAMP,
    withdrawn_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX one_current_consent_idx ON guardian_consents(athlete_id)
    WHERE status IN ('pending','granted');
CREATE TABLE status_transitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    actor_id INTEGER NOT NULL REFERENCES users(id),
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    reason TEXT NOT NULL,
    request_id TEXT NOT NULL,
    occurred_at TIMESTAMP NOT NULL
);
CREATE INDEX transitions_athlete_time_idx ON status_transitions(athlete_id, occurred_at, id);
CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id INTEGER NOT NULL REFERENCES users(id),
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id INTEGER NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('success','rejected','failed')),
    basis TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX audit_object_idx ON audit_events(object_type, object_id, created_at, id);
CREATE INDEX audit_actor_idx ON audit_events(actor_id, created_at);
`,
	},
	{
		Version: 2,
		Name:    "screening_and_training",
		SQL: `
CREATE TABLE health_screenings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    answers_json TEXT NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('pending','cleared','review_required')),
    reviewer_user_id INTEGER REFERENCES users(id),
    review_basis TEXT NOT NULL DEFAULT '',
    submitted_at TIMESTAMP NOT NULL,
    reviewed_at TIMESTAMP
);
CREATE INDEX screenings_athlete_idx ON health_screenings(athlete_id, submitted_at DESC);
CREATE TABLE baseline_assessments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    assessor_user_id INTEGER NOT NULL REFERENCES users(id),
    sequence INTEGER NOT NULL,
    endurance_score INTEGER NOT NULL CHECK (endurance_score BETWEEN 0 AND 100),
    strength_score INTEGER NOT NULL CHECK (strength_score BETWEEN 0 AND 100),
    mobility_score INTEGER NOT NULL CHECK (mobility_score BETWEEN 0 AND 100),
    resting_heart_rate INTEGER NOT NULL,
    conclusion TEXT NOT NULL,
    assessed_at TIMESTAMP NOT NULL,
    UNIQUE (athlete_id, sequence)
);
CREATE INDEX baseline_latest_idx ON baseline_assessments(athlete_id, assessed_at DESC);
CREATE TABLE prescriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    author_user_id INTEGER NOT NULL REFERENCES users(id),
    week_start TIMESTAMP NOT NULL,
    version INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft','published','superseded')),
    weekly_load_limit INTEGER NOT NULL,
    max_session_load INTEGER NOT NULL,
    min_recovery_hours INTEGER NOT NULL,
    strength_days INTEGER NOT NULL,
    basis TEXT NOT NULL,
    published_at TIMESTAMP,
    superseded_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (athlete_id, week_start, version)
);
CREATE UNIQUE INDEX one_published_week_idx ON prescriptions(athlete_id, week_start)
    WHERE status = 'published';
CREATE INDEX prescription_lookup_idx ON prescriptions(athlete_id, week_start, status);
CREATE TABLE strength_blocks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    prescription_id INTEGER NOT NULL REFERENCES prescriptions(id) ON DELETE CASCADE,
    day_offset INTEGER NOT NULL CHECK (day_offset BETWEEN 0 AND 6),
    muscle_group TEXT NOT NULL,
    sets INTEGER NOT NULL,
    repetitions INTEGER NOT NULL,
    intensity_rpe INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (prescription_id, day_offset, muscle_group)
);
CREATE TABLE activity_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    prescription_id INTEGER NOT NULL REFERENCES prescriptions(id),
    recorder_user_id INTEGER NOT NULL REFERENCES users(id),
    idempotency_key TEXT NOT NULL,
    occurred_at TIMESTAMP NOT NULL,
    recorded_at TIMESTAMP NOT NULL,
    duration_minutes INTEGER NOT NULL,
    perceived_effort INTEGER NOT NULL,
    load_units INTEGER NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('student','coach','correction')),
    late_entry INTEGER NOT NULL CHECK (late_entry IN (0,1)),
    supersedes_id INTEGER REFERENCES activity_logs(id),
    created_at TIMESTAMP NOT NULL,
    UNIQUE (athlete_id, idempotency_key)
);
CREATE INDEX activity_window_idx ON activity_logs(athlete_id, occurred_at, id);
CREATE TABLE load_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    activity_id INTEGER NOT NULL UNIQUE REFERENCES activity_logs(id) ON DELETE CASCADE,
    window_start TIMESTAMP NOT NULL,
    window_end TIMESTAMP NOT NULL,
    seven_day_load INTEGER NOT NULL,
    previous_load INTEGER NOT NULL,
    threshold INTEGER NOT NULL,
    risk_triggered INTEGER NOT NULL CHECK (risk_triggered IN (0,1)),
    calculated_at TIMESTAMP NOT NULL
);
CREATE INDEX load_snapshot_athlete_idx ON load_snapshots(athlete_id, window_end DESC);
`,
	},
	{
		Version: 3,
		Name:    "risk_reassessment_and_jobs",
		SQL: `
CREATE TABLE fatigue_reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    reporter_user_id INTEGER NOT NULL REFERENCES users(id),
    reported_for TIMESTAMP NOT NULL,
    fatigue_score INTEGER NOT NULL,
    soreness_score INTEGER NOT NULL,
    sleep_hours REAL NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    UNIQUE (athlete_id, reported_for)
);
CREATE TABLE risk_cases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    trigger_type TEXT NOT NULL,
    trigger_reference_id INTEGER NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('moderate','high','critical')),
    status TEXT NOT NULL CHECK (status IN ('open','acknowledged','resolved')),
    basis TEXT NOT NULL,
    assigned_advisor_id INTEGER REFERENCES users(id),
    resolution TEXT NOT NULL DEFAULT '',
    opened_at TIMESTAMP NOT NULL,
    acknowledged_at TIMESTAMP,
    resolved_at TIMESTAMP,
    version INTEGER NOT NULL DEFAULT 1,
    UNIQUE (trigger_type, trigger_reference_id)
);
CREATE INDEX open_risk_idx ON risk_cases(athlete_id, status, opened_at);
CREATE TABLE reassessments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    athlete_id INTEGER NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
    assessor_user_id INTEGER NOT NULL REFERENCES users(id),
    baseline_id INTEGER NOT NULL REFERENCES baseline_assessments(id),
    endurance_score INTEGER NOT NULL,
    strength_score INTEGER NOT NULL,
    mobility_score INTEGER NOT NULL,
    recommendation TEXT NOT NULL,
    basis TEXT NOT NULL,
    assessed_at TIMESTAMP NOT NULL
);
CREATE INDEX reassessment_latest_idx ON reassessments(athlete_id, assessed_at DESC);
CREATE TABLE worker_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','running','retry','succeeded','permanent_failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    available_at TIMESTAMP NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMP,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX runnable_jobs_idx ON worker_jobs(status, available_at, lease_until, id);
CREATE TABLE idempotency_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope TEXT NOT NULL,
    key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_json TEXT NOT NULL,
    resource_id INTEGER NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (scope, key)
);
CREATE INDEX idempotency_expiry_idx ON idempotency_keys(expires_at);
`,
	},
}
