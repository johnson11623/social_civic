-- name: UpsertAdminUnit :exec
INSERT INTO admin_units (
    level, code, iebc_code, name, display_name,
    parent_level, parent_code, registered_voters, source_version
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (level, code) DO UPDATE SET
    iebc_code         = EXCLUDED.iebc_code,
    name              = EXCLUDED.name,
    display_name      = EXCLUDED.display_name,
    parent_level      = EXCLUDED.parent_level,
    parent_code       = EXCLUDED.parent_code,
    registered_voters = EXCLUDED.registered_voters,
    source_version    = EXCLUDED.source_version,
    updated_at        = now()
WHERE (admin_units.iebc_code, admin_units.name, admin_units.display_name,
       admin_units.parent_level, admin_units.parent_code,
       admin_units.registered_voters, admin_units.source_version)
  IS DISTINCT FROM
      (EXCLUDED.iebc_code, EXCLUDED.name, EXCLUDED.display_name,
       EXCLUDED.parent_level, EXCLUDED.parent_code,
       EXCLUDED.registered_voters, EXCLUDED.source_version);

-- name: CountAdminUnitsByLevel :many
SELECT level, count(*)::int AS units
FROM admin_units
GROUP BY level
ORDER BY level;

-- name: ListAdminUnits :many
SELECT level, code, iebc_code, name, display_name,
       parent_code, registered_voters, source_version
FROM admin_units
ORDER BY level DESC, code;
