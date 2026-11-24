-- +goose Up
-- move server-owned allocations into primary ip_addresses rows
INSERT INTO ip_addresses (kind, project_id, location_id, allocation_id, server_id, family, ip, name, description, labels, auto_delete, created_at)
SELECT 'primary', c.project_id, c.location_id, c.id, c.server_id, c.family, c.ip,
  CASE WHEN c.dupes > 1 OR EXISTS (SELECT 1 FROM ip_addresses x WHERE x.project_id = c.project_id AND x.name = c.base)
    THEN left(c.server_name, 57 - length(c.id::text)) || '-' || c.family || '-' || c.id
    ELSE c.base
  END,
  '', '{}', true, c.created_at
FROM (
  SELECT a.id, a.ip, a.server_id, a.created_at, s.project_id, p.location_id, p.family, s.name AS server_name,
    rtrim(left(s.name, 58), '-') || '-' || p.family AS base,
    count(*) OVER (PARTITION BY s.project_id, rtrim(left(s.name, 58), '-') || '-' || p.family) AS dupes
  FROM ip_allocations a
  JOIN servers s ON s.id = a.server_id
  JOIN ip_pools p ON p.id = a.pool_id
  WHERE a.owner = 'server'
) c;
UPDATE ip_allocations SET owner = 'primary', server_id = NULL WHERE owner = 'server';
-- +goose StatementBegin
DO $$
DECLARE
  addrs bigint;
  allocs bigint;
  left_over bigint;
BEGIN
  SELECT count(*) INTO addrs FROM ip_addresses WHERE kind = 'primary';
  SELECT count(*) INTO allocs FROM ip_allocations WHERE owner = 'primary';
  SELECT count(*) INTO left_over FROM ip_allocations WHERE owner = 'server';
  IF addrs <> allocs OR left_over <> 0 THEN
    RAISE EXCEPTION 'primary IP migration count mismatch: % primary IPs, % primary allocations, % server allocations left', addrs, allocs, left_over;
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE
  n bigint;
BEGIN
  SELECT count(*) INTO n FROM ip_addresses WHERE kind = 'primary' AND server_id IS NULL;
  IF n > 0 THEN
    RAISE EXCEPTION '% unassigned primary IPs exist; delete or assign them before rolling back', n;
  END IF;
  SELECT count(*) INTO n FROM ip_addresses WHERE kind = 'primary' AND NOT auto_delete;
  IF n > 0 THEN
    RAISE EXCEPTION '% primary IPs marked keep exist; set auto_delete or delete them before rolling back', n;
  END IF;
  SELECT count(*) INTO n FROM ip_addresses
    WHERE kind = 'primary' AND (releasing_from_id IS NOT NULL OR pending_gen IS NOT NULL OR deleting);
  IF n > 0 THEN
    RAISE EXCEPTION '% primary IPs have a running action; wait for it to finish before rolling back', n;
  END IF;
END $$;
-- +goose StatementEnd
UPDATE ip_allocations a SET owner = 'server', server_id = x.server_id
  FROM ip_addresses x WHERE x.allocation_id = a.id AND x.kind = 'primary';
-- +goose StatementBegin
DO $$
DECLARE
  n bigint;
BEGIN
  SELECT count(*) INTO n FROM ip_allocations WHERE owner = 'primary';
  IF n > 0 THEN
    RAISE EXCEPTION '% primary allocations have no primary IP row; cannot roll back', n;
  END IF;
END $$;
-- +goose StatementEnd
DELETE FROM ip_addresses WHERE kind = 'primary';
