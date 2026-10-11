package parse

import (
	"bytes"
	"encoding/json"
)

// Record holds the parts of /db/aarecord_elasticsearch/<id>.json that
// book_details uses. Lists tolerate numbers and nulls in place of strings.
type Record struct {
	ID              string `json:"id"`
	FileUnifiedData struct {
		TitleBest               LooseString             `json:"title_best"`
		TitleAdditional         LooseStrings            `json:"title_additional"`
		AuthorBest              LooseString             `json:"author_best"`
		AuthorAdditional        LooseStrings            `json:"author_additional"`
		PublisherBest           LooseString             `json:"publisher_best"`
		PublisherAdditional     LooseStrings            `json:"publisher_additional"`
		EditionVariaBest        LooseString             `json:"edition_varia_best"`
		EditionVariaAdditional  LooseStrings            `json:"edition_varia_additional"`
		YearBest                LooseString             `json:"year_best"`
		ExtensionBest           LooseString             `json:"extension_best"`
		FilesizeBest            LooseString             `json:"filesize_best"`
		ContentTypeBest         LooseString             `json:"content_type_best"`
		StrippedDescriptionBest LooseString             `json:"stripped_description_best"`
		MostLikelyLanguageCodes LooseStrings            `json:"most_likely_language_codes"`
		AddedDateBest           LooseString             `json:"added_date_best"`
		OriginalFilenameBest    LooseString             `json:"original_filename_best"`
		HasAADownloads          LooseString             `json:"has_aa_downloads"`
		IdentifiersUnified      map[string]LooseStrings `json:"identifiers_unified"`
		ClassificationsUnified  map[string]LooseStrings `json:"classifications_unified"`
		Problems                []struct {
			Type             LooseString `json:"type"`
			Descr            LooseString `json:"descr"`
			BetterAarecordID LooseString `json:"better_aarecord_id"`
		} `json:"problems"`
	} `json:"file_unified_data"`
	SearchOnlyFields struct {
		SearchRecordSources LooseStrings `json:"search_record_sources"`
	} `json:"search_only_fields"`
	Additional struct {
		Path LooseString `json:"path"`
	} `json:"additional"`
}

// LooseString accepts a JSON string, number, boolean or null.
type LooseString string

func (s *LooseString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	switch {
	case bytes.Equal(data, []byte("null")):
		*s = ""
	case len(data) > 0 && data[0] == '"':
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = LooseString(value)
	default:
		*s = LooseString(data)
	}
	return nil
}

// LooseStrings accepts a JSON list of loose strings, or a single value.
type LooseStrings []string

func (s *LooseStrings) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '[' {
		var value LooseString
		if err := value.UnmarshalJSON(data); err != nil {
			return err
		}
		*s = nil
		if value != "" {
			*s = LooseStrings{string(value)}
		}
		return nil
	}
	var values []LooseString
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	*s = make(LooseStrings, 0, len(values))
	for _, value := range values {
		*s = append(*s, string(value))
	}
	return nil
}

// MD5Summary is the answer of /dyn/md5/summary/<md5>. Its account fields describe the
// session that asked, so requests carry a unique cachebust parameter: the
// archive's CDN would otherwise cache one member's answer for everyone.
type MD5Summary struct {
	ReportsCount        int `json:"reports_count"`
	CommentsCount       int `json:"comments_count"`
	ListsCount          int `json:"lists_count"`
	DownloadsTotal      int `json:"downloads_total"`
	GreatQualityCount   int `json:"great_quality_count"`
	DownloadsLeft       int `json:"downloads_left"`
	IsMember            int `json:"is_member"`
	DownloadStillActive int `json:"download_still_active"`
}

// FastDownloadAnswer is the body of /dyn/api/fast_download.json. A failed
// request sets Error and leaves DownloadURL null. AccountInfo is read before
// the request is counted, so it does not yet include the download it allows.
type FastDownloadAnswer struct {
	DownloadURL *string              `json:"download_url"`
	Error       string               `json:"error"`
	AccountInfo *FastDownloadAccount `json:"account_fast_download_info"`
}

// FastDownloadAccount is the account's fast download allowance.
type FastDownloadAccount struct {
	DownloadsLeft          int      `json:"downloads_left"`
	DownloadsPerDay        int      `json:"downloads_per_day"`
	RecentlyDownloadedMD5s []string `json:"recently_downloaded_md5s"`
}

// IsUpAnswer accepts the uptime endpoint's JSON object, such as
// {"aa_logged_in":0}. A DDoS-Guard page or another site's response fails.
func IsUpAnswer(body []byte) bool {
	var status map[string]json.RawMessage
	if err := json.Unmarshal(body, &status); err != nil {
		return false
	}
	_, ok := status["aa_logged_in"]
	return ok
}
