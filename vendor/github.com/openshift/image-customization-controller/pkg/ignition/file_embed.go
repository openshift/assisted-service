package ignition

import (
	ignition_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/vincent-petithory/dataurl"
)

func toDataUrl(text []byte) string {
	data := &dataurl.DataURL{
		MediaType: dataurl.MediaType{
			Type:    "text",
			Subtype: "plain",
		},
		Encoding: dataurl.EncodingASCII,
		Data:     text,
	}
	return data.String()
}

func ignitionFileEmbed(path string, mode int, overwrite bool, data []byte) ignition_types.File {
	source := toDataUrl(data)
	return ignition_types.File{
		Node: ignition_types.Node{Path: path, Overwrite: &overwrite},
		FileEmbedded1: ignition_types.FileEmbedded1{
			Contents: ignition_types.Resource{Source: &source},
			Mode:     &mode,
		},
	}
}

func ignitionFileEmbedAppend(path string, mode int, data []byte) ignition_types.File {
	source := toDataUrl(data)
	return ignition_types.File{
		Node: ignition_types.Node{Path: path},
		FileEmbedded1: ignition_types.FileEmbedded1{
			Append: []ignition_types.Resource{{Source: &source}},
			Mode:   &mode,
		},
	}
}
