package dnsproviders

import (
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/route53/route53iface"
)

// Route53 represnets a Route53 client
type Route53 struct {
	RecordSet      RecordSet
	HostedZoneID   string
	SVC            route53iface.Route53API
	SharedCreds    bool
	DeafultEnvVars bool
}

func (r Route53) getService() (route53iface.Route53API, error) {
	if r.SVC == nil {
		var config *aws.Config
		if r.SharedCreds {
			config = &aws.Config{
				Credentials: credentials.NewSharedCredentials("", "route53"),
			}
		} else if !r.DeafultEnvVars {
			config = &aws.Config{
				Credentials: credentials.NewStaticCredentials(
					os.Getenv("AWS_ACCESS_KEY_ID_ROUTE53"),
					os.Getenv("AWS_SECRET_ACCESS_KEY_ROUTE53"),
					""),
			}
		}
		sess, err := session.NewSession(config)
		if err != nil {
			return nil, err
		}
		r.SVC = route53.New(sess)
	}
	return r.SVC, nil
}

// CreateRecordSet creates a record set
func (r Route53) CreateRecordSet(recordSetName, recordSetValue string) (string, error) {
	return r.ChangeRecordSet("CREATE", recordSetName, recordSetValue)
}

// DeleteRecordSet deletes a record set
func (r Route53) DeleteRecordSet(recordSetName, recordSetValue string) (string, error) {
	return r.ChangeRecordSet("DELETE", recordSetName, recordSetValue)
}

// UpdateRecordSet updates a record set
func (r Route53) UpdateRecordSet(recordSetName, recordSetValue string) (string, error) {
	return r.ChangeRecordSet("UPSERT", recordSetName, recordSetValue)
}

// ChangeRecordSet change record set according to the specified action
func (r Route53) ChangeRecordSet(action, recordSetName, recordSetValue string) (string, error) {
	svc, err := r.getService()
	if err != nil {
		return "", err
	}

	input := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{
				{
					Action: aws.String(action),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String(recordSetName),
						ResourceRecords: []*route53.ResourceRecord{
							{
								Value: aws.String(recordSetValue),
							},
						},
						TTL:  aws.Int64(r.RecordSet.TTL),
						Type: aws.String(r.RecordSet.RecordSetType),
					},
				},
			},
		},
		HostedZoneId: aws.String(r.HostedZoneID),
	}

	result, err := svc.ChangeResourceRecordSets(input)
	return result.String(), err
}

// GetRecordSet returns a record set according to the specified name
func (r Route53) GetRecordSet(recordSetName string) (string, error) {
	svc, err := r.getService()
	if err != nil {
		return "", err
	}

	listParams := &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(r.HostedZoneID),
		MaxItems:        aws.String("1"),
		StartRecordName: aws.String(recordSetName),
		StartRecordType: aws.String(r.RecordSet.RecordSetType),
	}
	respList, err := svc.ListResourceRecordSets(listParams)
	if err != nil {
		return "", err
	}

	if len(respList.ResourceRecordSets) == 0 {
		// RecordSet not found
		return "", nil
	}

	recordSetNameAWSFormat := strings.Replace(recordSetName, "*", "\\052", 1) + "."
	recordSet := respList.ResourceRecordSets[0]
	if *recordSet.Type != r.RecordSet.RecordSetType ||
		recordSetNameAWSFormat != *recordSet.Name {
		// RecordSet not found
		return "", nil
	}

	return recordSet.String(), nil
}

// GetDomainName returns domain name of the associated hosted zone
func (r Route53) GetDomainName() (string, error) {
	svc, err := r.getService()
	if err != nil {
		return "", err
	}

	input := &route53.GetHostedZoneInput{
		Id: aws.String(r.HostedZoneID),
	}

	result, err := svc.GetHostedZone(input)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(*result.HostedZone.Name, "."), err
}
