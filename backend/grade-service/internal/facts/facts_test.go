package facts

import "testing"

func TestBuildKnowsWhatItCanEstablish(t *testing.T) {
	got := Build(Inputs{CodeAvailable: true, Files: 12, LOC: 3400, CIAvailable: true, CIConfigFiles: []string{".sourcecraft/ci.yaml"}, SecurityPolicyFound: true})

	if got.Files == nil || *got.Files != 12 || got.LOC == nil || *got.LOC != 3400 {
		t.Errorf("size = %v / %v, want 12 files and 3400 lines", got.Files, got.LOC)
	}
	if got.HasCI == nil || !*got.HasCI || got.HasSecurityPolicy == nil || !*got.HasSecurityPolicy {
		t.Errorf("ci = %v security = %v, want both true", got.HasCI, got.HasSecurityPolicy)
	}
}

func TestBuildReportsAnAbsentFileAsFalseOnlyWhenTheExportWasReadable(t *testing.T) {
	readable := Build(Inputs{CodeAvailable: true, CIAvailable: true})
	if readable.HasCI == nil || *readable.HasCI || readable.HasSecurityPolicy == nil || *readable.HasSecurityPolicy {
		t.Errorf("no files in a readable export must mean false: ci=%v security=%v", readable.HasCI, readable.HasSecurityPolicy)
	}

	unknown := Build(Inputs{})
	if unknown.Files != nil || unknown.LOC != nil || unknown.HasCI != nil || unknown.HasSecurityPolicy != nil {
		t.Errorf("unreadable exports must leave every fact unknown (nil), got %+v", unknown)
	}

	// Code readable but CI export unreadable: only CI stays unknown.
	partial := Build(Inputs{CodeAvailable: true, Files: 1, LOC: 1})
	if partial.HasCI != nil || partial.HasSecurityPolicy == nil {
		t.Errorf("partial = %+v, want CI unknown and the security policy known", partial)
	}
}
