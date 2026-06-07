SELECT setval(
    pg_get_serial_sequence('storage_scopes', 'id'),
    GREATEST((SELECT COALESCE(MAX(id), 1) FROM storage_scopes), 1),
    true
);
