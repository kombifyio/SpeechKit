package shortcuts

type Intent string

const (
	IntentNone       Intent = ""
	IntentQuickNote  Intent = "quick_note"
	IntentCopyLast   Intent = "copy_last"
	IntentInsertLast Intent = "insert_last"
	IntentSummarize  Intent = "summarize"
)

type Resolution struct {
	Intent  Intent
	Payload string
	Alias   string
}
