CREATE DATABASE IF NOT EXISTS agent_gateway CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE agent_gateway;

CREATE TABLE IF NOT EXISTS execution_world (
  id VARCHAR(64) PRIMARY KEY,
  generation BIGINT NOT NULL,
  tenant_id VARCHAR(128) NOT NULL,
  session_id VARCHAR(128) NOT NULL,
  runtime_type VARCHAR(32) NOT NULL,
  runtime_ref VARCHAR(128) NULL,
  resource_json JSON NOT NULL,
  status VARCHAR(32) NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_world_tenant_status (tenant_id,status),
  KEY idx_world_expiry (status,expires_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS execution (
  id VARCHAR(64) PRIMARY KEY,
  world_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(128) NOT NULL,
  session_id VARCHAR(128) NOT NULL,
  idempotency_key VARCHAR(255) NULL,
  status VARCHAR(32) NOT NULL,
  attempt INT NOT NULL,
  argv_json JSON NOT NULL,
  workdir VARCHAR(1024) NOT NULL,
  env_json JSON NOT NULL,
  timeout_sec INT NOT NULL,
  output_limit BIGINT NOT NULL,
  started_at DATETIME(6) NULL,
  finished_at DATETIME(6) NULL,
  exit_code INT NULL,
  termination_reason VARCHAR(64) NULL,
  lease_version BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uk_execution_idempotency (tenant_id,idempotency_key),
  KEY idx_execution_world_status (world_id,status)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS process (
  id VARCHAR(64) PRIMARY KEY,
  execution_id VARCHAR(64) NOT NULL,
  world_id VARCHAR(64) NOT NULL,
  worker_id VARCHAR(128) NOT NULL,
  pid INT NOT NULL,
  process_group INT NOT NULL,
  status VARCHAR(32) NOT NULL,
  started_at DATETIME(6) NOT NULL,
  finished_at DATETIME(6) NULL,
  exit_code INT NULL,
  signal VARCHAR(32) NULL,
  KEY idx_process_execution (execution_id),
  KEY idx_process_world_status (world_id,status)
) ENGINE=InnoDB;
