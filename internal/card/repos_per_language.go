package card

import (
	"github.com/tiennm99/ghglance/internal/github"
	"github.com/tiennm99/ghglance/internal/theme"
)

type reposPerLanguageCard struct{}

func (reposPerLanguageCard) Filename() string { return "repos-per-language.svg" }

func (reposPerLanguageCard) SVG(p *github.Profile, t theme.Theme) ([]byte, error) {
	return renderDonutCard("Repos Per Language", p.ReposByLanguage, t), nil
}
