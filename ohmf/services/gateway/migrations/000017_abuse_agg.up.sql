-- Migration: create abuse_scores table if not exists
CREATE TABLE IF NOT EXISTS abuse_scores (
  phone_e164 TEXT PRIMARY KEY,
  score INT NOT NULL
);
