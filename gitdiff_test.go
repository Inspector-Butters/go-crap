package main

import (
	"reflect"
	"testing"
)

func TestParseUnifiedDiff(t *testing.T) {
	diff := []byte(`diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -2,2 +2,3 @@
 line
+line
@@ -10 +11,0 @@
-deleted
diff --git a/old.go b/new.go
--- a/old.go
+++ b/new.go
@@ -1 +1 @@
-old
+new
`)
	got, err := parseUnifiedDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	want := changeSet{
		"a.go":   {{Start: 2, End: 4}, {Start: 10, End: 11}},
		"new.go": {{Start: 1, End: 1}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseUnifiedDiff() = %#v, want %#v", got, want)
	}
}

func TestMergeLineRanges(t *testing.T) {
	got := mergeLineRanges([]lineRange{{Start: 8, End: 9}, {Start: 2, End: 4}, {Start: 5, End: 6}})
	want := []lineRange{{Start: 2, End: 6}, {Start: 8, End: 9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeLineRanges() = %#v, want %#v", got, want)
	}
}
