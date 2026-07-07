ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write', 'profiles:read', 'schemas:read', 'schemas:write',
    'api_keys:read', 'api_keys:write', 'privacy:write', 'operations:read', 'operations:write',
    'users:read', 'users:write', 'roles:read', 'roles:write'
];

UPDATE api_keys
SET scopes = array_append(scopes, 'operations:write')
WHERE scopes @> ARRAY['operations:read']::text[]
  AND NOT scopes @> ARRAY['operations:write']::text[];
