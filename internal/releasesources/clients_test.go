package releasesources

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

type RequestResponseParameters struct {
	Version         string
	CPUArchitecture string
	Channel         string
	Response        string
}

type QueryParameters struct {
	Channel string
	Arch    string
}

func getValidSupportLevelResponse() string {
	supportLevelResponse := `{
		"data": [
		  {
			"uuid": "0964595a-151e-4240-8a62-31e6c3730226",
			"name": "OpenShift Container Platform 4",
			"former_names": [],
			"show_last_minor_release": false,
			"show_final_minor_release": false,
			"is_layered_product": false,
			"all_phases": [
			  {
				"name": "General availability",
				"ptype": "normal",
				"tooltip": null,
				"display_name": "General availability",
				"additional_text": null
			  },
			  {
				"name": "Full support",
				"ptype": "normal",
				"tooltip": null,
				"display_name": "Full support ends",
				"additional_text": null
			  },
			  {
				"name": "Maintenance support",
				"ptype": "normal",
				"tooltip": null,
				"display_name": "Maintenance support ends",
				"additional_text": null
			  },
			  {
				"name": "Extended update support",
				"ptype": "normal",
				"tooltip": null,
				"display_name": "Extended update support ends",
				"additional_text": null
			  },
			  {
				"name": "Extended life phase",
				"ptype": "extended",
				"tooltip": "The Extended Life Cycle Phase (ELP) is the post-retirement time period. We require that customers running Red Hat Enterprise Linux products beyond their retirement continue to have active subscriptions which ensures that they continue receiving access to all previously released content, documentation, and knowledge base articles as well as receive limited technical support. As there are no bug fixes, security fixes, hardware enablement, or root cause analysis available during the Extended Life Phase, customers may choose to purchase the Extended Life Cycle Support Add-On during the Extended Life Phase, which will provide them with critical impact security fixes and selected urgent priority bug fixes.",
				"display_name": "Extended life phase ends",
				"additional_text": null
			  }
			],
			"versions": [
			  {
				"name": "4.14",
				"type": "Full Support",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2023-10-31T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "4.15 GA + 3 months",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2025-05-01T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "2025-10-31T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.13",
				"type": "Full Support",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2023-05-17T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2024-01-31T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2024-11-17T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string"
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string"
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.12",
				"type": "Maintenance Support",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2023-01-17T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2023-08-17T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2024-07-17T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "2025-01-17T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.11",
				"type": "Maintenance Support",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2022-08-10T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2023-04-17T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2024-02-10T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.10",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2022-03-10T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2022-11-10T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2023-09-10T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.9",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2021-10-18T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2022-06-10T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2023-04-18T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.8",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2021-07-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2022-01-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2023-01-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "2023-04-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.7",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2021-02-24T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2021-10-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2022-08-24T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.6 EUS",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2020-10-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2021-03-24T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2022-10-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": "",
					"superscript": "9"
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.6",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2020-10-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2021-03-24T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2021-10-18T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.5",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2020-07-13T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2020-11-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2021-07-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.4",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2020-05-05T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2020-08-13T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2021-02-24T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.3",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2020-01-23T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2020-06-05T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2020-10-27T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.2",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2019-10-16T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2020-02-23T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2020-07-13T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  },
			  {
				"name": "4.1",
				"type": "End of life",
				"last_minor_release": null,
				"final_minor_release": null,
				"extra_header_value": null,
				"additional_text": "",
				"phases": [
				  {
					"name": "General availability",
					"date": "2019-06-04T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Full support",
					"date": "2019-11-16T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Maintenance support",
					"date": "2020-05-05T00:00:00.000Z",
					"date_format": "date",
					"additional_text": ""
				  },
				  {
					"name": "Extended update support",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  },
				  {
					"name": "Extended life phase",
					"date": "N/A",
					"date_format": "string",
					"additional_text": ""
				  }
				],
				"extra_dependences": []
			  }
			],
			"footnote": "",
			"link": "https://access.redhat.com/support/policy/updates/openshift/",
			"policies": "https://access.redhat.com/site/support/policy/updates/openshift/policies/"
		  }
		]
	  }`

	return supportLevelResponse
}

func getExpectedSupportLevelsGraph(openshiftMajorVersion string) (*SupportLevelGraph, error) {
	if openshiftMajorVersion == "4" {
		return &SupportLevelGraph{
			Data: []Data{
				{
					Versions: []Version{
						{Name: "4.14", Type: "Full Support"},
						{Name: "4.13", Type: "Full Support"},
						{Name: "4.12", Type: "Maintenance Support"},
						{Name: "4.11", Type: "Maintenance Support"},
						{Name: "4.10", Type: "End of life"},
						{Name: "4.9", Type: "End of life"},
						{Name: "4.8", Type: "End of life"},
						{Name: "4.7", Type: "End of life"},
						{Name: "4.6 EUS", Type: "End of life"},
						{Name: "4.6", Type: "End of life"},
						{Name: "4.5", Type: "End of life"},
						{Name: "4.4", Type: "End of life"},
						{Name: "4.3", Type: "End of life"},
						{Name: "4.2", Type: "End of life"},
						{Name: "4.1", Type: "End of life"},
					},
				},
			},
		}, nil
	}

	return nil, errors.New("")
}

func getValidRequestResponseParameters() []RequestResponseParameters {
	// Should match defaultReleaseSources
	return []RequestResponseParameters{
		{
			Version:         "4.10",
			CPUArchitecture: common.AMD64CPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelStable,
			Response: `{
				"nodes": [
					{
						"version": "4.10.1",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.12",
			CPUArchitecture: common.AMD64CPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelStable,
			Response: `{
				"nodes": [
					{
						"version": "4.12.1",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.13",
			CPUArchitecture: common.AMD64CPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelStable,
			Response: `{
				"nodes": [
					{
						"version": "4.13.1",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					},
					{
						"version": "4.13.17",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "foo-bar"
						}
					},
					{
						"version": "4.12.15",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.13",
			CPUArchitecture: common.S390xCPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelStable,
			Response: `{
				"nodes": [
					{
						"version": "4.13.1",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					},
					{
						"version": "4.13.19",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "foo-bar"
						}
					},
					{
						"version": "4.12.40",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.14",
			CPUArchitecture: common.AMD64CPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelStable,
			Response: `{
				"nodes": [
					{
						"version": "4.14.0",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHSA-foo-bar"
						}
					},
					{
						"version": "4.14.1",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHBA-foo-bar"
						}
					},
					{
						"version": "4.13.40",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.14",
			CPUArchitecture: common.AMD64CPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelCandidate,
			Response: `{
				"nodes": [
					{
						"version": "4.14.0-rc.1",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHSA-foo-bar"
						}
					},
					{
						"version": "4.14.0-ec.2",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHBA-foo-bar"
						}
					},
					{
						"version": "4.13.10",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.14",
			CPUArchitecture: common.PowerCPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelStable,
			Response: `{
				"nodes": [
					{
						"version": "4.13.5",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHBA-foo-bar"
						}
					},
					{
						"version": "4.13.15",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.14",
			CPUArchitecture: common.PowerCPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelCandidate,
			Response: `{
				"nodes": [
					{
						"version": "4.14.0",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHSA-foo-bar"
						}
					},
					{
						"version": "4.14.1",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHBA-foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.15",
			CPUArchitecture: common.AMD64CPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelCandidate,
			Response: `{
				"nodes": [
					{
						"version": "4.15.0-ec.2",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHSA-foo-bar"
						}
					}
				]
			}`,
		},
		{
			Version:         "4.16",
			CPUArchitecture: common.MultiCPUArchitecture,
			Channel:         common.OpenshiftReleaseChannelCandidate,
			Response: `{
				"nodes": [
					{
						"version": "4.16.0-ec.2",
						"payload": "quay.io/openshift-release-dev/ocp-release@sha256:foo-bar",
						"metadata": {
							"io.openshift.upgrades.graph.previous.remove_regex": "foo-bar",
							"io.openshift.upgrades.graph.release.channels": "foo-bar",
							"io.openshift.upgrades.graph.release.manifestref": "sha256:foo-bar",
							"url": "https://access.redhat.com/errata/RHSA-foo-bar"
						}
					}
				]
			}`,
		},
	}
}

//gocyclo:ignore
func getExpectedReleasesGraphForValidParams(channel, openshiftVersion, cpuArchitecture string) (*ReleaseGraph, error) {
	if channel == common.OpenshiftReleaseChannelStable && openshiftVersion == "4.10" && cpuArchitecture == common.AMD64CPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.10.1"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelStable && openshiftVersion == "4.12" && cpuArchitecture == common.AMD64CPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.12.1"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelStable && openshiftVersion == "4.13" && cpuArchitecture == common.AMD64CPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.13.1"},
				{Version: "4.13.17"},
				{Version: "4.12.15"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelStable && openshiftVersion == "4.13" && cpuArchitecture == common.S390xCPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.13.1"},
				{Version: "4.13.19"},
				{Version: "4.12.40"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelStable && openshiftVersion == "4.14" && cpuArchitecture == common.AMD64CPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.14.0"},
				{Version: "4.14.1"},
				{Version: "4.13.40"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelCandidate && openshiftVersion == "4.14" && cpuArchitecture == common.AMD64CPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.14.0-rc.1"},
				{Version: "4.14.0-ec.2"},
				{Version: "4.13.10"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelStable && openshiftVersion == "4.14" && cpuArchitecture == common.PowerCPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.13.5"},
				{Version: "4.13.15"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelCandidate && openshiftVersion == "4.14" && cpuArchitecture == common.PowerCPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.14.0"},
				{Version: "4.14.1"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelCandidate && openshiftVersion == "4.14" && cpuArchitecture == common.AMD64CPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.14.1"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelCandidate && openshiftVersion == "4.15" && cpuArchitecture == common.AMD64CPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.15.0-ec.2"},
			},
		}, nil
	}

	if channel == common.OpenshiftReleaseChannelCandidate && openshiftVersion == "4.16" && cpuArchitecture == common.MultiCPUArchitecture {
		return &ReleaseGraph{
			Nodes: []Node{
				{Version: "4.16.0-ec.2"},
			},
		}, nil
	}

	return nil, errors.New("")
}

var _ = Describe("Test clients", func() {
	Describe("Test releases client", func() {
		Context("Test getReleases", func() {
			It("Should be successfull with valid request/response params", func() {

				var responseMatcher = map[QueryParameters]string{}

				for _, parameters := range getValidRequestResponseParameters() {
					responseMatcher[QueryParameters{
						Channel: fmt.Sprintf("%s-%s", parameters.Channel, parameters.Version),
						Arch:    parameters.CPUArchitecture,
					}] = parameters.Response
				}

				releasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					queryParameters := r.URL.Query()

					if response, ok := responseMatcher[QueryParameters{
						Channel: queryParameters["channel"][0],
						Arch:    queryParameters["arch"][0],
					}]; ok {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(response))
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
				defer releasesServer.Close()

				client := OpenShiftReleasesAPIClient{BaseUrl: releasesServer.URL}

				for _, params := range getValidRequestResponseParameters() {
					expectedReleasesGraph, err := getExpectedReleasesGraphForValidParams(params.Channel, params.Version, params.CPUArchitecture)
					Expect(err).ToNot(HaveOccurred())

					releasesGraph, err := client.GetReleases(params.Channel, params.Version, params.CPUArchitecture)
					Expect(err).ToNot(HaveOccurred())
					Expect(releasesGraph).To(Equal(expectedReleasesGraph))
				}
			})

			It("Should cause an error with invalid response from server", func() {
				var responseMatcher = map[QueryParameters]string{}

				for _, parameters := range getValidRequestResponseParameters() {
					responseMatcher[QueryParameters{
						Channel: fmt.Sprintf("%s-%s", parameters.Channel, parameters.Version),
						Arch:    parameters.CPUArchitecture,
					}] = parameters.Response
				}

				releasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					queryParameters := r.URL.Query()

					if queryParameters["channel"][0] == "stable-4.14" && queryParameters["arch"][0] == common.PowerCPUArchitecture {
						w.WriteHeader(http.StatusNotFound)
						return
					}

					if response, ok := responseMatcher[QueryParameters{
						Channel: queryParameters["channel"][0],
						Arch:    queryParameters["arch"][0],
					}]; ok {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(response))
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
				defer releasesServer.Close()

				client := OpenShiftReleasesAPIClient{BaseUrl: releasesServer.URL}
				releasesGraph, err := client.GetReleases(common.OpenshiftReleaseChannelStable, "4.14", common.PowerCPUArchitecture)
				Expect(err).To(HaveOccurred())
				Expect(releasesGraph).To(BeNil())
			})

			It("Should cause an error with valid response but unparsable body from server", func() {
				var responseMatcher = map[QueryParameters]string{}

				for _, parameters := range getValidRequestResponseParameters() {
					responseMatcher[QueryParameters{
						Channel: fmt.Sprintf("%s-%s", parameters.Channel, parameters.Version),
						Arch:    parameters.CPUArchitecture,
					}] = parameters.Response
				}

				releasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					queryParameters := r.URL.Query()

					if queryParameters["channel"][0] == "stable-4.14" && queryParameters["arch"][0] == common.PowerCPUArchitecture {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte("invalid-response"))
						return
					}

					if response, ok := responseMatcher[QueryParameters{
						Channel: queryParameters["channel"][0],
						Arch:    queryParameters["arch"][0],
					}]; ok {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(response))
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
				defer releasesServer.Close()

				client := OpenShiftReleasesAPIClient{BaseUrl: releasesServer.URL}
				releasesGraph, err := client.GetReleases(common.OpenshiftReleaseChannelStable, "4.14", common.PowerCPUArchitecture)
				Expect(err).To(HaveOccurred())
				Expect(releasesGraph).To(BeNil())
			})
		})
	})

	var _ = Describe("Test support levels client", func() {
		Context("Test getSupportLevels", func() {
			It("Should be successfull with valid request/response params", func() {
				supportLevelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					queryParameters := r.URL.Query()

					if queryParameters["name"][0] == "Openshift Container Platform 4" {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(getValidSupportLevelResponse()))
						return
					}

					w.WriteHeader(http.StatusNotFound)
				}))
				defer supportLevelServer.Close()

				client := OpenShiftSupportLevelAPIClient{BaseUrl: supportLevelServer.URL}
				supportLevelGraph, err := client.GetSupportLevels("4")
				Expect(err).ToNot(HaveOccurred())

				expectedSupportLevelGraph, err := getExpectedSupportLevelsGraph("4")
				Expect(err).ToNot(HaveOccurred())
				Expect(supportLevelGraph).To(Equal(expectedSupportLevelGraph))
			})

			It("Should cause an error with invalid response from server", func() {
				supportLevelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
				defer supportLevelServer.Close()

				client := OpenShiftSupportLevelAPIClient{BaseUrl: supportLevelServer.URL}
				supportLevelGraph, err := client.GetSupportLevels("4")
				Expect(err).To(HaveOccurred())
				Expect(supportLevelGraph).To(BeNil())
			})

			It("Should cause an error with valid response but unparsable body from server", func() {
				supportLevelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("invalid-response"))
				}))
				defer supportLevelServer.Close()

				client := OpenShiftSupportLevelAPIClient{BaseUrl: supportLevelServer.URL}
				supportLevelGraph, err := client.GetSupportLevels("4")
				Expect(err).To(HaveOccurred())
				Expect(supportLevelGraph).To(BeNil())
			})
		})
	})
})
