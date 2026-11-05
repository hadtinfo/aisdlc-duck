package spec

import (
	"reflect"
	"testing"
)

func TestMarkdownSectionsOrderCommentsAndCRLF(t *testing.T) {
	md := "intro\r\n## B <!-- x -->\r\nb1\r\n## A\r\n<!-- c\r\n -->a1\r\n## B\r\nb2\r\n"
	names, body := MarkdownSections(md)
	if !reflect.DeepEqual(names, []string{"B", "A"}) {
		t.Fatalf("names = %q", names)
	}
	if body["A"] != "a1\n" || body["B"] != "b1\nb2\n\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestOpenQuestionsList(t *testing.T) {
	md := "## Câu hỏi mở\n- [ ] Một?\n* [ ]   Hai?  \n- [x] Xong\n- [ ]\ntext\n"
	got := OpenQuestions(md)
	want := []string{"Một?", "Hai?", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if OpenQuestions("## Khác\n- [ ] x\n") != nil {
		t.Fatal("không có mục thì nil")
	}
}
