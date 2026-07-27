package gateway

import "testing"

func TestStateStoreDescribeCoverage(t *testing.T) {
	if _, err := NewSerializedStateStore(nil); err == nil {
		t.Fatal("nil serialized store was accepted")
	}
	var unconfigured SerializedStateStore
	if unconfigured.Describe() != "serialized:unconfigured" {
		t.Fatal("unconfigured serialized store description changed")
	}
	var nilSerialized *SerializedStateStore
	if _, _, err := nilSerialized.LoadInto(nil); err == nil {
		t.Fatal("nil serialized store load was accepted")
	}
	if _, err := nilSerialized.SaveFrom(nil); err == nil {
		t.Fatal("nil serialized store save was accepted")
	}
	file, err := NewFileStateStore("/tmp/rdev-gateway.json")
	if err != nil {
		t.Fatal(err)
	}
	postgres, err := NewPostgresStateStore("service=rdev-test")
	if err != nil {
		t.Fatal(err)
	}
	redis, err := NewRedisStreamStateStore("redis://127.0.0.1:6379")
	if err != nil {
		t.Fatal(err)
	}
	s3, err := NewS3CompatibleStateStore("s3://bucket/prefix")
	if err != nil {
		t.Fatal(err)
	}
	for _, store := range []StateStore{file, postgres, redis, s3} {
		if store.Describe() == "" {
			t.Fatalf("empty store description for %T", store)
		}
	}
	serialized, err := NewSerializedStateStore(file)
	if err != nil || serialized.Describe() == "" {
		t.Fatalf("serialized store = %v, err=%v", serialized, err)
	}
}
