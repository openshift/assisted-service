package dnsproviders

type Provider interface {
	CreateRecordSet(recordSetName, recordSetValue string) (string, error)
	UpdateRecordSet(recordSetName, recordSetValue string) (string, error)
	DeleteRecordSet(recordSetName, recordSetValue string) (string, error)
	GetRecordSet(recordSetName string) (string, error)
	GetDomainName() (string, error)
}

type RecordSet struct {
	RecordSetType string
	TTL           int64
}
