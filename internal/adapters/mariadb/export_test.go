package mariadb

// SetAfterMigrationLock replaces the hook run once the migration lock is held.
func SetAfterMigrationLock(f func()) (restore func()) {
	old := afterMigrationLock
	afterMigrationLock = f
	return func() { afterMigrationLock = old }
}
