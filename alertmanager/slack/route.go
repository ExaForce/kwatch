package slack

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// route sends events matching all label regexes to a set of channels
type route struct {
	labels   map[string]*regexp.Regexp
	channels []string
}

// matches reports whether every label regex in the route matches the
// corresponding event label. Regexes are unanchored; use ^...$ to match
// the full value.
func (r *route) matches(labels map[string]string) bool {
	for key, re := range r.labels {
		value, ok := labels[key]
		if !ok || !re.MatchString(value) {
			return false
		}
	}
	return true
}

type rawRoute struct {
	Labels   map[string]string `yaml:"labels"`
	Channels []string          `yaml:"channels"`
}

// parseRoutes converts the untyped routes config into compiled routes
func parseRoutes(raw interface{}) ([]route, error) {
	if raw == nil {
		return nil, nil
	}

	b, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid routes config: %w", err)
	}

	var rawRoutes []rawRoute
	if err := yaml.Unmarshal(b, &rawRoutes); err != nil {
		return nil, fmt.Errorf("invalid routes config: %w", err)
	}

	routes := make([]route, 0, len(rawRoutes))
	for i, rr := range rawRoutes {
		if len(rr.Labels) == 0 {
			return nil, fmt.Errorf("route %d has no labels", i)
		}
		if len(rr.Channels) == 0 {
			return nil, fmt.Errorf("route %d has no channels", i)
		}

		labels := make(map[string]*regexp.Regexp, len(rr.Labels))
		for key, pattern := range rr.Labels {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf(
					"route %d has invalid regex for label %q: %w", i, key, err)
			}
			labels[key] = re
		}

		routes = append(routes, route{
			labels:   labels,
			channels: rr.Channels,
		})
	}

	return routes, nil
}
