package harness

import (
	"context"
	"reflect"
	"testing"
)

func TestDumpSchema(t *testing.T) {
	url := freshDB(t)
	dir := t.TempDir()
	writeIn(t, dir, "00001_transfer.sql", `-- +goose Up
CREATE TABLE transfer (id bigint NOT NULL, note varchar(100), FROM_ACCOUNT text NOT NULL, amount numeric(10,2));
CREATE TYPE mood AS ENUM ('a','b');
CREATE TABLE Extra (m mood, tags text[], at timestamptz NOT NULL);
CREATE SCHEMA other;
CREATE TABLE other.hidden (x int);
`)
	if err := Migrate(context.Background(), url, dir); err != nil {
		t.Fatal(err)
	}
	snap, err := DumpSchema(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.Tables["goose_db_version"]; ok {
		t.Error("phải bỏ goose_db_version")
	}
	if _, ok := snap.Tables["hidden"]; ok {
		t.Error("chỉ lấy schema public")
	}
	want := map[string][]string{
		"transfer": {"amount:NUMERIC:NULL", "from_account:TEXT:NOT NULL", "id:BIGINT:NOT NULL", "note:CHARACTER VARYING:NULL"},
		"extra":    {"at:TIMESTAMP WITH TIME ZONE:NOT NULL", "m:MOOD:NULL", "tags:_TEXT:NULL"},
	}
	if !reflect.DeepEqual(snap.Tables, want) {
		t.Errorf("got %#v", snap.Tables)
	}
}

func TestDumpSchemaIncludesAddedMigration(t *testing.T) {
	url := freshDB(t)
	main, drafts := t.TempDir(), t.TempDir()
	writeIn(t, main, "00001_t.sql", "-- +goose Up\nCREATE TABLE transfer (id bigint NOT NULL);\n")
	writeIn(t, drafts, "00002_note.sql", "-- +goose Up\nALTER TABLE transfer ADD COLUMN note varchar(50);\n")
	if err := Migrate(context.Background(), url, main, drafts); err != nil {
		t.Fatal(err)
	}
	snap, err := DumpSchema(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Tables["transfer"]; !reflect.DeepEqual(got, []string{"id:BIGINT:NOT NULL", "note:CHARACTER VARYING:NULL"}) {
		t.Errorf("got %v", got)
	}
}
