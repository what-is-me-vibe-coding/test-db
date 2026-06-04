package query

import (
	"testing"

	"github.com/xwb1989/sqlparser"
)

// TestConvertSQLValUnsupportedType tests the default branch in convertSQLVal
// for SQLVal types that are not IntVal, FloatVal, or StrVal.
func TestConvertSQLValUnsupportedType(t *testing.T) {
	p := NewParser()
	// ValArg type (e.g., :param placeholder) is not handled by convertSQLVal
	val := &sqlparser.SQLVal{Type: sqlparser.ValArg, Val: []byte("param")}
	_, err := p.convertSQLVal(val)
	if err == nil {
		t.Error("expected error for unsupported SQLVal type, got nil")
	}
}

// TestConvertSQLValIntOverflow tests convertSQLVal with an integer that
// overflows int64, triggering the ParseInt error branch.
func TestConvertSQLValIntOverflow(t *testing.T) {
	p := NewParser()
	val := &sqlparser.SQLVal{Type: sqlparser.IntVal, Val: []byte("999999999999999999999999999")}
	_, err := p.convertSQLVal(val)
	if err == nil {
		t.Error("expected error for int overflow, got nil")
	}
}

// TestParseUint64NonSQLVal tests parseUint64 with a non-SQLVal expression,
// triggering the type assertion failure branch.
func TestParseUint64NonSQLVal(t *testing.T) {
	p := NewParser()
	// Pass a ColName instead of SQLVal
	expr := &sqlparser.ColName{Name: sqlparser.NewColIdent("col")}
	_, err := p.parseUint64(expr)
	if err == nil {
		t.Error("expected error for non-SQLVal expression, got nil")
	}
}

// TestParseUint64InvalidValue tests parseUint64 with an invalid uint64 string,
// triggering the ParseUint error branch.
func TestParseUint64InvalidValue(t *testing.T) {
	p := NewParser()
	// Negative number in SQLVal
	val := &sqlparser.SQLVal{Type: sqlparser.IntVal, Val: []byte("-1")}
	_, err := p.parseUint64(val)
	if err == nil {
		t.Error("expected error for negative uint64, got nil")
	}
}

// TestConvertComparisonOpUnsupported tests convertComparisonOp with
// an unsupported comparison operator string.
func TestConvertComparisonOpUnsupported(t *testing.T) {
	p := NewParser()
	_, err := p.convertComparisonOp("NOT IN")
	if err == nil {
		t.Error("expected error for unsupported comparison operator, got nil")
	}
}
