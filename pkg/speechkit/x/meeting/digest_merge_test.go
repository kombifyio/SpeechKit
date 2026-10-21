package meeting

import "testing"

func TestMergeDigestsConcatenatesListsInPartOrder(t *testing.T) {
	merged, err := MergeDigests([]string{
		`{"topics":["budget"],"decisions":[],"speakerReferences":{"Anna":"mic"}}`,
		`{"topics":["launch"],"decisions":["ship Friday"],"speakerReferences":{"Ben":"system"}}`,
		`{"topics":["risks"]}`,
	})
	if err != nil {
		t.Fatalf("MergeDigests: %v", err)
	}
	want := `{"decisions":["ship Friday"],"speakerReferences":{"Anna":"mic"},"topics":["budget","launch","risks"]}`
	if merged != want {
		t.Fatalf("merged = %s\nwant   %s", merged, want)
	}
	if _, err := MergeDigests([]string{`{"topics":[]}`, `not json`}); err == nil {
		t.Fatal("a part that is not JSON must fail the merge")
	}
	if _, err := MergeDigests(nil); err == nil {
		t.Fatal("no parts must not produce an empty digest")
	}
}
