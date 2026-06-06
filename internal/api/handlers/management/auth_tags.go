package management

import (
	"sort"
	"strings"
)

type channelGroupChannelDetail struct {
	Name              string `json:"name"`
	Source            string `json:"source,omitempty"`
	Disabled          bool   `json:"disabled,omitempty"`
	disabledAuthority int
	DefaultTags       []string `json:"default_tags"`
	CustomTags        []string `json:"custom_tags"`
	HiddenDefaultTags []string `json:"hidden_default_tags"`
	DisplayTags       []string `json:"display_tags"`
}

func uniqueSortedChannelDetails(values []channelGroupChannelDetail) []channelGroupChannelDetail {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]channelGroupChannelDetail, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if name == "" {
			continue
		}
		value.Name = name
		if value.DefaultTags == nil {
			value.DefaultTags = []string{}
		}
		if value.CustomTags == nil {
			value.CustomTags = []string{}
		}
		if value.HiddenDefaultTags == nil {
			value.HiddenDefaultTags = []string{}
		}
		if value.DisplayTags == nil {
			value.DisplayTags = []string{}
		}
		key := strings.ToLower(name)
		existing, ok := seen[key]
		if !ok {
			seen[key] = value
			continue
		}

		disabled := mergedChannelDisabled(existing, value)
		if tagPayloadScore(value) >= tagPayloadScore(existing) {
			value.Disabled = disabled
			value.disabledAuthority = max(existing.disabledAuthority, value.disabledAuthority)
			seen[key] = value
		} else {
			existing.Disabled = disabled
			existing.disabledAuthority = max(existing.disabledAuthority, value.disabledAuthority)
			seen[key] = existing
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]channelGroupChannelDetail, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func tagPayloadScore(value channelGroupChannelDetail) int {
	return len(value.DisplayTags)*100 + len(value.DefaultTags)*10 + len(value.CustomTags)
}

func mergedChannelDisabled(existing channelGroupChannelDetail, value channelGroupChannelDetail) bool {
	if existing.disabledAuthority > value.disabledAuthority {
		return existing.Disabled
	}
	if value.disabledAuthority > existing.disabledAuthority {
		return value.Disabled
	}
	return existing.Disabled || value.Disabled
}
