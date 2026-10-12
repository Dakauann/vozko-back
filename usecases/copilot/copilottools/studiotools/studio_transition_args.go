package studiotools

type studioRevealArgs struct {
	Softness       *float64 `json:"softness,omitempty" desc:"gradientWipe, venetianBlinds, blockDissolve e radialWipe: borda suave do que aparece, de 0 (corte seco) a 1 (bem suave)"`
	PieceSize      *float64 `json:"piece_size,omitempty" desc:"venetianBlinds: largura de cada lâmina; blockDissolve: lado de cada bloco; fração do lado menor do quadro (ou do clipe, na entrada e na saída), de 0.01 a 1"`
	WipeMap        string   `json:"wipe_map,omitempty" enum:"linear,radial,diamond,clouds" desc:"gradientWipe: o mapa que decide a ordem em que a imagem aparece, do escuro para o claro; linear segue angle, radial e diamond saem de origin_x e origin_y, clouds aparece em manchas como nuvens; não vai junto com wipe_map_media_id"`
	WipeMapMediaID string   `json:"wipe_map_media_id,omitempty" desc:"gradientWipe: uma imagem da biblioteca usada como mapa; as partes escuras aparecem primeiro e as claras por último (reverse inverte); não vai junto com wipe_map"`
}
