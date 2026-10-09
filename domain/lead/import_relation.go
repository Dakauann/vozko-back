package lead

import (
	"strings"

	"vozko/domain/shared"
)

var relationKindWords = map[string]RelationKind{
	"conjuge": KindSpouse, "esposa": KindSpouse, "esposo": KindSpouse, "marido": KindSpouse, "mulher": KindSpouse,
	"companheiro": KindPartner, "companheira": KindPartner, "parceiro": KindPartner, "parceira": KindPartner,
	"namorado": KindPartner, "namorada": KindPartner,
	"pai": KindParent, "mae": KindParent, "padrasto": KindParent, "madrasta": KindParent,
	"filho": KindChild, "filha": KindChild, "enteado": KindChild, "enteada": KindChild,
	"irmao": KindSibling, "irma": KindSibling,
	"avo": KindGrandparent, "avos": KindGrandparent,
	"neto": KindGrandchild, "neta": KindGrandchild,
	"tio": KindUncleAunt, "tia": KindUncleAunt,
	"sobrinho": KindNephewNiece, "sobrinha": KindNephewNiece,
	"primo": KindCousin, "prima": KindCousin,
	"sogro": KindInLaw, "sogra": KindInLaw, "genro": KindInLaw, "nora": KindInLaw, "cunhado": KindInLaw, "cunhada": KindInLaw,
	"parente": KindRelative, "familiar": KindRelative, "outro": KindRelative,
}

func ParseRelationKind(raw string) (RelationKind, bool) {
	word := strings.Join(strings.Fields(shared.FoldForMatch(raw)), " ")
	if word == "" {
		return KindRelative, true
	}
	if kind, ok := relationKindWords[word]; ok {
		return kind, true
	}
	kind := RelationKind(word)
	if kind.Valid() && kind.Dimension() == DimensionFamily {
		return kind, true
	}
	return "", false
}
