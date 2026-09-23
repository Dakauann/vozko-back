import io
import json
import os

SRC = 'C:/Users/dakau/Documents/projects/vozko_main/vozko-front/src/i18n/messages'
OUT = 'C:/Users/dakau/Documents/projects/vozko_main/vozko-back/usecases/report/renderers/labels_generated.go'

EXTRA_NAMESPACES = {
    'actorKind': ('metricsOps', 'common'),
    'presence': ('metricsOps', 'common'),
    'channel': ('metricsOps', 'common'),
}


def flatten(node, prefix=''):
    out = {}
    for key, value in node.items():
        path = prefix + '.' + key if prefix else key
        if isinstance(value, dict):
            out.update(flatten(value, path))
        elif isinstance(value, str):
            out[path] = value
    return out


def go_quote(value):
    return json.dumps(value, ensure_ascii=False)


lines = [
    '// Code generated from the frontend message catalogue. DO NOT EDIT.',
    '',
    'package report_renderers',
    '',
    'func ExportLabels() *StaticLabels {',
    '\tlabels := NewStaticLabels("pt")',
]

for locale in ('pt', 'en', 'es', 'de'):
    path = os.path.join(SRC, '%s.json' % locale)
    data = json.load(io.open(path, encoding='utf-8'))
    export = data.get('metricsOps', {}).get('export', {})
    common = data.get('metricsOps', {}).get('common', {})

    table = flatten(export)

    for key in ('yes', 'no', 'noDepartment', 'noFunnel', 'title', 'button'):
        if key in export and isinstance(export[key], str):
            table[key] = export[key]

    for kind_key, label_key in (('human', 'human'), ('ai', 'ai'), ('system', 'system')):
        if label_key in common:
            table['actorKind.' + kind_key] = common[label_key]
    table.setdefault('actorKind.human', 'Human')
    table.setdefault('actorKind.ai', 'AI')
    table.setdefault('actorKind.system', 'System')

    for presence_key, label_key in (('online', 'online'), ('on_call', 'onCall'), ('offline', 'offline')):
        if label_key in common:
            table['presence.' + presence_key] = common[label_key]

    for channel_key in ('whatsapp', 'unofficial_whatsapp', 'instagram', 'telegram', 'voice'):
        if channel_key in common:
            table['channel.' + channel_key] = common[channel_key]

    lines.append('\tlabels.Add(%s, Labels{' % go_quote(locale))
    lines.append('\t\t"metricsOps.export": {')
    for key in sorted(table):
        lines.append('\t\t\t%s: %s,' % (go_quote(key), go_quote(table[key])))
    lines.append('\t\t},')
    lines.append('\t})')

lines.append('\treturn labels')
lines.append('}')
lines.append('')

io.open(OUT, 'w', encoding='utf-8', newline='\n').write('\n'.join(lines))
print('wrote', OUT, len(lines), 'lines')
