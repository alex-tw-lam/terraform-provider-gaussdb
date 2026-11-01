package gaussdb

import (
	"testing"

	"github.com/blang/semver"
)

func TestParseServerVersion(t *testing.T) {
	tcs := []struct {
		name        string
		versionStr  string
		wantVersion string
		wantGaussDB bool
	}{
		{
			name:        "vanilla postgresql",
			versionStr:  "PostgreSQL 16.15 (Debian 16.15-1.pgdg13+2) on x86_64-pc-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit",
			wantVersion: "16.15.0",
			wantGaussDB: false,
		},
		{
			name:        "community opengauss",
			versionStr:  "(openGauss 5.0.0 build ) compiled at 2023-04-18 09:47:39 commit 0 last mr   on x86_64-unknown-linux-gnu, compiled by g++ (GCC) 7.3.0, 64-bit",
			wantVersion: "5.0.0",
			wantGaussDB: true,
		},
		{
			name:        "enterprise gaussdb kernel",
			versionStr:  "(GaussDB Kernel V506.0.0.SPC0500 build 1234.567) compiled at 2025-06-01 10:00:00 commit 8f9e0d1 last mr on x86_64-unknown-linux-gnu, compiled by g++ (GCC) 10.3.0, 64-bit",
			wantVersion: "506.0.0",
			wantGaussDB: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			version, isGaussDB, err := parseServerVersion(tc.versionStr)
			if err != nil {
				t.Fatalf("parseServerVersion(%q) failed: %v", tc.name, err)
			}
			if want := semver.MustParse(tc.wantVersion); !version.Equals(want) {
				t.Errorf("parseServerVersion(%q) version = %s, want %s", tc.name, version, want)
			}
			if isGaussDB != tc.wantGaussDB {
				t.Errorf("parseServerVersion(%q) isGaussDB = %v, want %v", tc.name, isGaussDB, tc.wantGaussDB)
			}
		})
	}
}

func TestParseServerVersionNoVersionToken(t *testing.T) {
	if _, _, err := parseServerVersion("no version here at all"); err == nil {
		t.Fatal("expected an error when no version-looking token exists")
	}
}
