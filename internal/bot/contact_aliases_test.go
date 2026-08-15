package bot

import "testing"

func TestParseContactAliasDefinition(t *testing.T) {
	tests := []struct {
		input       string
		alias       string
		contactType string
		contact     string
	}{
		{"Лиза это @liza1231", "лиза", "telegram", "liza1231"},
		{"запомни Маша = +357 99 999999", "маша", "phone", "+35799999999"},
		{"/alias Катя = @Kate", "катя", "telegram", "kate"},
	}
	for _, test := range tests {
		alias, contactType, contact, ok := parseContactAliasDefinition(test.input)
		if !ok || alias != test.alias || contactType != test.contactType || contact != test.contact {
			t.Fatalf("parseContactAliasDefinition(%q) = %q, %q, %q, %v", test.input, alias, contactType, contact, ok)
		}
	}
}

func TestResolveContactAliasUnderstandsRussianInflection(t *testing.T) {
	aliases := []ContactAlias{
		{Alias: "лиза", ContactType: "telegram", Contact: "liza1231"},
		{Alias: "маша", ContactType: "phone", Contact: "+35799999999"},
	}
	got, ok := resolveContactAlias("запиши Лизу завтра на эпиляцию", aliases)
	if !ok || got.Alias != "лиза" || got.Contact != "liza1231" {
		t.Fatalf("resolved alias = %#v, ok=%v", got, ok)
	}
	if _, ok := resolveContactAlias("запиши клиента завтра", aliases); ok {
		t.Fatal("unrelated text must not resolve an alias")
	}
}
