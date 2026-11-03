package panel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestRoomFor(t *testing.T) {
	const gb = int64(1) << 30
	for _, tc := range []struct {
		name              string
		size, free, total int64
		ok                bool
	}{
		{"small disk with room", gb, 7 * gb, 38 * gb, true},
		{"small disk would fill", 13 * gb / 2, 7 * gb, 38 * gb, false},
		{"big disk mostly full is fine", gb, 30 * gb, 500 * gb, true},
		{"big disk keeps a gigabyte", gb, gb + gb/2, 500 * gb, false},
		{"tiny disk reserves a twentieth", 0, gb / 2, 4 * gb, true},
	} {
		if err := roomFor(tc.size, tc.free, tc.total); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func TestCheckVolumeArchive(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := bytes.Repeat([]byte("data"), 10000)
	if err := tw.WriteHeader(&tar.Header{Name: "./a", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	if err := checkVolumeArchive(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("a good archive failed: %v", err)
	}
	if err := checkVolumeArchive(bytes.NewReader(buf.Bytes()[:buf.Len()/2])); err == nil {
		t.Fatal("a truncated archive passed")
	}
	if err := checkVolumeArchive(bytes.NewReader([]byte("not an archive"))); err == nil {
		t.Fatal("garbage passed")
	}
}

func TestVolumeBackupsLeaveWithTheirApp(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	for _, id := range []string{"a1", "a2"} {
		if _, err := sqlDB.Exec(`INSERT INTO volume_backups(`+volumeBackupCols+`) VALUES(?,'shop','data','t',?,1,'manual',1,'','',0)`, id, id+".tar.gz"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO volume_backups(` + volumeBackupCols + `) VALUES('b1','other','data','t','b1.tar.gz',1,'manual',1,'','',0)`); err != nil {
		t.Fatal(err)
	}
	removeAppVolumeBackups("shop")
	if got := listVolumeBackups("shop", "data"); len(got) != 0 {
		t.Fatalf("left behind: %+v", got)
	}
	if got := listVolumeBackups("other", "data"); len(got) != 1 {
		t.Fatalf("removed another app's backup: %+v", got)
	}
}
