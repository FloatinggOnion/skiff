package panel

import "testing"

func TestVolumeOffsiteOwnersDoNotCollide(t *testing.T) {
	openTestDB(t)
	if volumeOffsiteOwner("x-data", "y") == volumeOffsiteOwner("x", "data-y") {
		t.Fatal("different app and volume pairs must have different owners")
	}
	old := volumeBackupDirName("x", "data-y")
	for _, k := range []string{"volumes/x/data-y/1-a.tar.gz", "volumes/x-data/y/1-b.tar.gz"} {
		if _, err := sqlDB.Exec(`INSERT INTO offsite_objects(db_id,key,created) VALUES(?,?,1)`, old, k); err != nil {
			t.Fatal(err)
		}
	}

	adoptVolumeOffsiteRows(volumeOffsiteOwner("x", "data-y"), "x", "data-y")

	count := func(owner string) (n int) {
		_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM offsite_objects WHERE db_id=?`, owner).Scan(&n)
		return
	}
	if count(volumeOffsiteOwner("x", "data-y")) != 1 || count(old) != 1 {
		t.Fatalf("only the matching copy should move: new=%d old=%d", count(volumeOffsiteOwner("x", "data-y")), count(old))
	}
	adoptVolumeOffsiteRows(volumeOffsiteOwner("x-data", "y"), "x-data", "y")
	if count(volumeOffsiteOwner("x-data", "y")) != 1 || count(old) != 0 {
		t.Fatal("the other pair should find its own copy")
	}
}
