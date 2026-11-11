package address

import "testing"

func TestAFieldTableHoldsEachNameOnce(t *testing.T) {
	if _, err := NewFieldTable(FieldRow{Name: "title"}, FieldRow{Name: "body"}, FieldRow{Name: "title"}); err == nil {
		t.Error("a table holding title twice was made")
	}
	table, err := NewFieldTable(FieldRow{Name: "title"}, FieldRow{Name: "body"})
	if err != nil || len(table.Rows()) != 2 {
		t.Errorf("a table of title and body is %v, %v", table.Rows(), err)
	}
}
