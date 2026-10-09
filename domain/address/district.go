package address

import (
	"strings"

	"vozko/domain/shared"
)

var districtPunctuation = strings.NewReplacer(".", " ", ",", " ", "-", " ", "/", " ", "'", " ", "(", " ", ")", " ")

var districtAbbreviations = map[string]string{
	"jd":   "jardim",
	"vl":   "vila",
	"pq":   "parque",
	"res":  "residencial",
	"cj":   "conjunto",
	"conj": "conjunto",
	"st":   "setor",
	"sta":  "santa",
	"sto":  "santo",
	"nsa":  "nossa",
	"sra":  "senhora",
}

func DistrictKey(name string) string {
	words := strings.Fields(districtPunctuation.Replace(shared.FoldForMatch(name)))
	for i, word := range words {
		if word == "n" && i+1 < len(words) && words[i+1] == "sra" {
			words[i] = "nossa"
			continue
		}
		if expanded, ok := districtAbbreviations[word]; ok {
			words[i] = expanded
		}
	}
	return strings.Join(words, " ")
}
