"""
Reusable Vozko API documentation PDF builder.

Matches the exact design of vozko_api_autenticacao.pdf:
- Full-blue cover with centered text
- TOC page with numbered items and gray separators
- Content pages: numbered section titles, endpoint badges,
  parameter tables (blue header, alternating rows), dark code blocks,
  error tables, info boxes with blue left border
- Header bar ("Vozko Global API | <topic>") + page numbers
- Footer line ("Vozko Global - Documentacao Publica da API")

Usage:
    from vozko_pdf_builder import VozkoPdfBuilder
    pdf = VozkoPdfBuilder("Templates", "Guia Completo do Sistema de Templates",
                          output_path="docs/vozko_api_templates.pdf")
    pdf.toc([("Visao Geral", "Conceitos e categorias"), ...])
    pdf.section("1. Visao Geral")
    pdf.text("Texto de introducao...")
    pdf.endpoint("GET", "/whatsapp/templates", rate_limit="...")
    pdf.param_table([("campo", "tipo", "sim", "descricao"), ...])
    pdf.code_block('{\\n  "key": "value" \\n}')
    pdf.error_table([("400", "Corpo invalido", "Invalid request body"), ...])
    pdf.info_box("Texto de aviso importante.")
    pdf.build()
"""

from reportlab.lib.pagesizes import A4
from reportlab.lib.units import mm
from reportlab.lib.colors import HexColor, Color
from reportlab.lib.styles import ParagraphStyle
from reportlab.platypus import (
    SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle,
    KeepTogether, PageBreak, Flowable,
)
from reportlab.lib.enums import TA_LEFT, TA_CENTER, TA_JUSTIFY
from reportlab.pdfgen import canvas
import os

# ── Exact colors from vozko_api_autenticacao.pdf ────────────────
BLUE         = HexColor('#2563EB')     # (0.1451, 0.3882, 0.9216)
DARK_NAVY    = HexColor('#1E2941')     # (0.1176, 0.1608, 0.2314) code bg
LIGHT_BG     = HexColor('#F9FAFB')     # (0.9765, 0.9804, 0.9843) rows / info
WHITE        = HexColor('#FFFFFF')
BLACK        = HexColor('#000000')
GREEN_BADGE  = HexColor('#16A34A')     # (0.0863, 0.6392, 0.2902) POST badge
ORANGE_BADGE = HexColor('#EA580C')     # PATCH / PUT
RED_BADGE    = HexColor('#DC2626')     # DELETE
GRAY_LINE    = HexColor('#E5E7EB')     # (0.898, 0.9059, 0.9216) separators
TEXT_DARK    = HexColor('#1E293B')
TEXT_MUTED   = HexColor('#64748B')

W, H = A4                              # 595 x 842 pt
LEFT   = 28                            # left margin (pt)
RIGHT  = 539                           # right edge (pt)
USABLE = RIGHT - LEFT                  # ~511 pt

METHOD_COLORS = {
    'GET':    BLUE,
    'POST':   GREEN_BADGE,
    'PUT':    ORANGE_BADGE,
    'PATCH':  ORANGE_BADGE,
    'DELETE': RED_BADGE,
}


class _NumberedCanvas(canvas.Canvas):
    """Adds header bar, page number and footer on every page."""

    def __init__(self, *args, topic='', date_str='Abril 2026', **kwargs):
        self._topic = topic
        self._date_str = date_str
        canvas.Canvas.__init__(self, *args, **kwargs)
        self._saved = []

    def showPage(self):
        self._saved.append(dict(self.__dict__))
        self._startPage()

    def save(self):
        total = len(self._saved)
        for state in self._saved:
            self.__dict__.update(state)
            self._draw_extras(total)
            canvas.Canvas.showPage(self)
        canvas.Canvas.save(self)

    def _draw_extras(self, total):
        pg = self._pageNumber
        if pg == 1:
            return  # cover has no header/footer

        # --- top header bar (full width, 6pt tall) ---
        self.setFillColor(BLUE)
        self.setStrokeColor(BLUE)
        self.rect(0, H - 6, W, 6, fill=1, stroke=0)

        # header text on the bar
        self.setFont('Helvetica-Bold', 9)
        self.setFillColor(TEXT_DARK)
        self.drawString(LEFT, H - 22, f'Vozko Global API  |  {self._topic}')
        self.drawRightString(RIGHT, H - 22, f'Pagina {pg}')

        # --- bottom footer ---
        self.setStrokeColor(GRAY_LINE)
        self.setLineWidth(0.5)
        self.line(43, 43, W - 43, 43)
        self.setFont('Helvetica', 7)
        self.setFillColor(TEXT_MUTED)
        self.drawCentredString(W / 2, 30,
                               'Vozko Global  -  Documentacao Publica da API')


class VozkoPdfBuilder:
    """Builds a Vozko-styled API documentation PDF."""

    def __init__(self, topic: str, subtitle: str, output_path: str,
                 version: str = 'Versao 1.0', date_str: str = 'Abril 2026',
                 base_url: str = 'https://api.vozkoglobal.com'):
        self._topic = topic
        self._subtitle = subtitle
        self._output = output_path
        self._version = version
        self._date = date_str
        self._base_url = base_url
        self._elements: list = []
        self._init_styles()

    # ── Styles ───────────────────────────────────────────────────
    def _init_styles(self):
        self.s_section = ParagraphStyle(
            'section', fontName='Helvetica-Bold', fontSize=18,
            textColor=TEXT_DARK, leading=22, spaceBefore=0, spaceAfter=2 * mm,
        )
        self.s_subsection = ParagraphStyle(
            'subsection', fontName='Helvetica-Bold', fontSize=12,
            textColor=TEXT_DARK, leading=16, spaceBefore=4 * mm, spaceAfter=2 * mm,
        )
        self.s_body = ParagraphStyle(
            'body', fontName='Helvetica', fontSize=10,
            textColor=TEXT_DARK, leading=14, spaceAfter=2 * mm,
        )
        self.s_body_small = ParagraphStyle(
            'body_small', fontName='Helvetica', fontSize=9,
            textColor=TEXT_DARK, leading=13, spaceAfter=2 * mm,
        )
        self.s_code = ParagraphStyle(
            'code', fontName='Courier', fontSize=9,
            textColor=WHITE, leading=13,
        )
        self.s_tbl_hdr = ParagraphStyle(
            'tbl_hdr', fontName='Helvetica-Bold', fontSize=8,
            textColor=WHITE, leading=11,
        )
        self.s_tbl_cell = ParagraphStyle(
            'tbl_cell', fontName='Helvetica', fontSize=8,
            textColor=TEXT_DARK, leading=11,
        )
        self.s_tbl_cell_bold = ParagraphStyle(
            'tbl_cell_b', fontName='Helvetica-Bold', fontSize=8,
            textColor=TEXT_DARK, leading=11,
        )
        self.s_endpoint = ParagraphStyle(
            'endpoint', fontName='Courier-Bold', fontSize=12,
            textColor=TEXT_DARK, leading=16,
        )
        self.s_method = ParagraphStyle(
            'method', fontName='Helvetica-Bold', fontSize=9,
            textColor=WHITE, alignment=TA_CENTER, leading=12,
        )
        self.s_info = ParagraphStyle(
            'info', fontName='Helvetica', fontSize=9,
            textColor=TEXT_DARK, leading=13,
        )
        self.s_bullet = ParagraphStyle(
            'bullet', fontName='Helvetica-Bold', fontSize=9,
            textColor=WHITE, alignment=TA_CENTER, leading=12,
        )
        self.s_bullet_text = ParagraphStyle(
            'bullet_text', fontName='Helvetica', fontSize=10,
            textColor=TEXT_DARK, leading=14,
        )
        self.s_toc_num = ParagraphStyle(
            'toc_num', fontName='Helvetica-Bold', fontSize=11,
            textColor=TEXT_DARK, leading=14,
        )
        self.s_toc_title = ParagraphStyle(
            'toc_title', fontName='Helvetica-Bold', fontSize=11,
            textColor=TEXT_DARK, leading=14,
        )
        self.s_toc_desc = ParagraphStyle(
            'toc_desc', fontName='Helvetica', fontSize=9,
            textColor=TEXT_MUTED, leading=12,
        )

    # ── Cover page ───────────────────────────────────────────────
    def _cover(self):
        """Full-blue cover page drawn directly on the canvas."""
        topic = self._topic
        subtitle = self._subtitle
        version = self._version
        date = self._date
        base_url = self._base_url

        class CoverFlowable(Flowable):
            """Custom flowable that fills the entire page with the cover."""
            width = 1
            height = 1

            def wrap(self, availWidth, availHeight):
                return (1, 1)

            def draw(self):
                c = self.canv
                # Full-page blue background
                c.setFillColor(BLUE)
                c.rect(0 - LEFT, 0 - 55, W, H, fill=1, stroke=0)

                # "Vozko Global" — centered, bold 42pt
                c.setFont('Helvetica-Bold', 42)
                c.setFillColor(WHITE)
                c.drawCentredString(W / 2 - LEFT, H - 55 - 120, 'Vozko Global')

                # "Documentacao da API" — centered, 16pt
                c.setFont('Helvetica', 16)
                c.drawCentredString(W / 2 - LEFT, H - 55 - 150, 'Documentacao da API')

                # White separator line
                mid = W / 2 - LEFT
                c.setStrokeColor(WHITE)
                c.setLineWidth(1)
                c.line(mid - 127, H - 55 - 200, mid + 127, H - 55 - 200)

                # Topic title — centered, bold 28pt
                c.setFont('Helvetica-Bold', 28)
                c.drawCentredString(mid, H - 55 - 260, topic)

                # Subtitle
                c.setFont('Helvetica', 16)
                c.drawCentredString(mid, H - 55 - 290, subtitle)

                # Version + date
                c.setFont('Helvetica', 11)
                c.drawCentredString(mid, H - 55 - 380, f'{version}  |  {date}')

                # "Documentacao Publica - Vozko Global"
                c.drawCentredString(mid, H - 55 - 400, 'Documentacao Publica  -  Vozko Global')

                # Base URL
                c.setFont('Helvetica', 10)
                c.drawCentredString(mid, H - 55 - 420, base_url)

        self._elements.append(CoverFlowable())
        self._elements.append(PageBreak())

    # ── Table of Contents ────────────────────────────────────────
    def toc(self, items: list[tuple[str, str]]):
        """
        Add a table-of-contents page.
        items: list of (title, description) tuples.
        """
        self._elements.append(Paragraph(
            'Sumario',
            ParagraphStyle('toc_h', fontName='Helvetica-Bold', fontSize=22,
                           textColor=TEXT_DARK, leading=28, spaceAfter=6 * mm),
        ))

        # Blue line under "Sumario"
        line = Table([['']], colWidths=[USABLE], rowHeights=[1])
        line.setStyle(TableStyle([
            ('BACKGROUND', (0, 0), (-1, -1), BLUE),
        ]))
        self._elements.append(line)
        self._elements.append(Spacer(1, 4 * mm))

        # Each TOC row: number | title | description, separated by gray lines
        for i, (title, desc) in enumerate(items, 1):
            row_data = [
                [Paragraph(f'{i}.', self.s_toc_num),
                 Paragraph(title, self.s_toc_title),
                 Paragraph(desc, self.s_toc_desc)],
            ]
            row_t = Table(row_data, colWidths=[25, 160, USABLE - 185])
            row_t.setStyle(TableStyle([
                ('VALIGN', (0, 0), (-1, -1), 'MIDDLE'),
                ('LEFTPADDING', (0, 0), (-1, -1), 0),
                ('RIGHTPADDING', (0, 0), (-1, -1), 0),
                ('TOPPADDING', (0, 0), (-1, -1), 4),
                ('BOTTOMPADDING', (0, 0), (-1, -1), 4),
            ]))
            self._elements.append(row_t)

            # Gray separator
            sep = Table([['']], colWidths=[USABLE], rowHeights=[0.5])
            sep.setStyle(TableStyle([
                ('BACKGROUND', (0, 0), (-1, -1), GRAY_LINE),
            ]))
            self._elements.append(sep)
            self._elements.append(Spacer(1, 1 * mm))

        self._elements.append(PageBreak())

    # ── Section title ────────────────────────────────────────────
    def section(self, title: str):
        """Numbered section title (e.g. "1. Visao Geral"), with blue underline."""
        self._elements.append(Paragraph(title, self.s_section))
        line = Table([['']], colWidths=[USABLE], rowHeights=[1])
        line.setStyle(TableStyle([
            ('BACKGROUND', (0, 0), (-1, -1), BLUE),
        ]))
        self._elements.append(line)
        self._elements.append(Spacer(1, 3 * mm))

    def subsection(self, title: str):
        """Subsection heading (e.g. "Corpo da Requisicao")."""
        self._elements.append(Paragraph(title, self.s_subsection))

    # ── Body text ────────────────────────────────────────────────
    def text(self, t: str):
        """Regular body paragraph."""
        self._elements.append(Paragraph(t, self.s_body))

    def text_small(self, t: str):
        """Smaller body paragraph (9pt)."""
        self._elements.append(Paragraph(t, self.s_body_small))

    # ── Endpoint block ───────────────────────────────────────────
    def endpoint(self, method: str, path: str, permission: str = '',
                 rate_limit: str = ''):
        """
        Endpoint badge: colored method badge + path in Courier-Bold.
        permission: e.g. "AUTENTICADO", "whatsapp_templates.read", "ADMIN"
        """
        color = METHOD_COLORS.get(method.upper(), BLUE)

        # Method badge (rounded rect with white text)
        badge = Table(
            [[Paragraph(method.upper(), self.s_method)]],
            colWidths=[45], rowHeights=[18],
        )
        badge.setStyle(TableStyle([
            ('BACKGROUND', (0, 0), (-1, -1), color),
            ('ROUNDEDCORNERS', [3, 3, 3, 3]),
            ('VALIGN', (0, 0), (-1, -1), 'MIDDLE'),
            ('ALIGN', (0, 0), (-1, -1), 'CENTER'),
            ('LEFTPADDING', (0, 0), (-1, -1), 0),
            ('RIGHTPADDING', (0, 0), (-1, -1), 0),
            ('TOPPADDING', (0, 0), (-1, -1), 0),
            ('BOTTOMPADDING', (0, 0), (-1, -1), 0),
        ]))

        endpoint_row = Table(
            [[badge, Spacer(8, 1), Paragraph(path, self.s_endpoint)]],
            colWidths=[50, 10, USABLE - 60],
        )
        endpoint_row.setStyle(TableStyle([
            ('VALIGN', (0, 0), (-1, -1), 'MIDDLE'),
            ('LEFTPADDING', (0, 0), (-1, -1), 0),
            ('RIGHTPADDING', (0, 0), (-1, -1), 0),
            ('TOPPADDING', (0, 0), (-1, -1), 0),
            ('BOTTOMPADDING', (0, 0), (-1, -1), 0),
        ]))

        # Wrap in light-bg box
        box = Table([[endpoint_row]], colWidths=[USABLE])
        box.setStyle(TableStyle([
            ('BACKGROUND', (0, 0), (-1, -1), LIGHT_BG),
            ('LEFTPADDING', (0, 0), (-1, -1), 12),
            ('RIGHTPADDING', (0, 0), (-1, -1), 12),
            ('TOPPADDING', (0, 0), (-1, -1), 10),
            ('BOTTOMPADDING', (0, 0), (-1, -1), 10),
        ]))

        keep_items = [box, Spacer(1, 2 * mm)]

        # Permission badge (rendered right after the endpoint box)
        if permission:
            perm_width = max(len(permission) * 6 + 16, 80)
            perm_badge = Table(
                [[Paragraph(permission, self.s_method)]],
                colWidths=[perm_width], rowHeights=[16],
            )
            perm_badge.setStyle(TableStyle([
                ('BACKGROUND', (0, 0), (-1, -1), BLUE),
                ('ROUNDEDCORNERS', [2, 2, 2, 2]),
                ('VALIGN', (0, 0), (-1, -1), 'MIDDLE'),
                ('ALIGN', (0, 0), (-1, -1), 'CENTER'),
                ('LEFTPADDING', (0, 0), (-1, -1), 6),
                ('RIGHTPADDING', (0, 0), (-1, -1), 6),
            ]))
            keep_items.append(perm_badge)
            keep_items.append(Spacer(1, 2 * mm))

        self._elements.append(KeepTogether(keep_items))

        # Rate limit info box
        if rate_limit:
            self.info_box(f'Rate Limit: {rate_limit}')

    # ── Parameter / data table ───────────────────────────────────
    def param_table(self, rows: list[tuple], headers: tuple = None,
                    col_widths: list = None):
        """
        Table with blue header row and alternating light/white rows.
        rows: list of tuples (one per row).
        headers: tuple of column names. Defaults to
                 ("Campo", "Tipo", "Obrigatorio", "Descricao").
        """
        if headers is None:
            headers = ('Campo', 'Tipo', 'Obrigatorio', 'Descricao')

        ncols = len(headers)
        if col_widths is None:
            if ncols == 4:
                col_widths = [90, 55, 70, USABLE - 215]
            elif ncols == 3:
                col_widths = [60, USABLE * 0.38, USABLE * 0.50]
            else:
                col_widths = [USABLE / ncols] * ncols

        hdr = [Paragraph(h, self.s_tbl_hdr) for h in headers]
        body = []
        for row in rows:
            cells = []
            for j, cell in enumerate(row):
                st = self.s_tbl_cell_bold if j == 0 else self.s_tbl_cell
                cells.append(Paragraph(str(cell), st))
            body.append(cells)

        data = [hdr] + body
        t = Table(data, colWidths=col_widths, repeatRows=1)

        style_cmds = [
            ('BACKGROUND', (0, 0), (-1, 0), BLUE),
            ('TEXTCOLOR', (0, 0), (-1, 0), WHITE),
            ('GRID', (0, 0), (-1, -1), 0.5, GRAY_LINE),
            ('VALIGN', (0, 0), (-1, -1), 'TOP'),
            ('LEFTPADDING', (0, 0), (-1, -1), 6),
            ('RIGHTPADDING', (0, 0), (-1, -1), 6),
            ('TOPPADDING', (0, 0), (-1, -1), 5),
            ('BOTTOMPADDING', (0, 0), (-1, -1), 5),
        ]
        # Alternating row backgrounds
        for i in range(len(body)):
            bg = LIGHT_BG if i % 2 == 0 else WHITE
            style_cmds.append(('BACKGROUND', (0, i + 1), (-1, i + 1), bg))

        t.setStyle(TableStyle(style_cmds))
        self._elements.append(KeepTogether([t, Spacer(1, 3 * mm)]))

    # ── Code block ───────────────────────────────────────────────
    def code_block(self, code: str):
        """Dark navy code block with Courier 9pt white text."""
        lines = code.strip().split('\n')
        formatted = '<br/>'.join(
            l.replace(' ', '&nbsp;').replace('<', '&lt;').replace('>', '&gt;')
            for l in lines
        )
        p = Paragraph(formatted, self.s_code)
        t = Table([[p]], colWidths=[USABLE])
        t.setStyle(TableStyle([
            ('BACKGROUND', (0, 0), (-1, -1), DARK_NAVY),
            ('LEFTPADDING', (0, 0), (-1, -1), 14),
            ('RIGHTPADDING', (0, 0), (-1, -1), 14),
            ('TOPPADDING', (0, 0), (-1, -1), 10),
            ('BOTTOMPADDING', (0, 0), (-1, -1), 10),
            ('ROUNDEDCORNERS', [2, 2, 2, 2]),
        ]))
        self._elements.append(KeepTogether([t, Spacer(1, 3 * mm)]))

    # ── Error table ──────────────────────────────────────────────
    def error_table(self, rows: list[tuple],
                    headers: tuple = ('Status', 'Situacao', 'Mensagem')):
        """Shortcut to param_table with error-appropriate defaults."""
        if len(headers) == 3:
            cw = [60, USABLE * 0.40, USABLE * 0.48]
        elif len(headers) == 2:
            cw = [60, USABLE - 60]
        else:
            cw = None
        self.param_table(rows, headers=headers, col_widths=cw)

    # ── Info box (light bg + blue left bar) ──────────────────────
    def info_box(self, text: str):
        """Light-background box with blue left accent bar."""
        p = Paragraph(text, self.s_info)

        # Inner content
        inner = Table([[p]], colWidths=[USABLE - 20])
        inner.setStyle(TableStyle([
            ('LEFTPADDING', (0, 0), (-1, -1), 10),
            ('RIGHTPADDING', (0, 0), (-1, -1), 6),
            ('TOPPADDING', (0, 0), (-1, -1), 8),
            ('BOTTOMPADDING', (0, 0), (-1, -1), 8),
        ]))

        # Blue left bar (9pt wide) + content
        bar = Table([['']], colWidths=[9], rowHeights=[1])
        bar.setStyle(TableStyle([
            ('BACKGROUND', (0, 0), (-1, -1), BLUE),
        ]))

        box = Table([[bar, inner]], colWidths=[9, USABLE - 9])
        box.setStyle(TableStyle([
            ('BACKGROUND', (0, 0), (-1, -1), LIGHT_BG),
            ('LEFTPADDING', (0, 0), (-1, -1), 0),
            ('RIGHTPADDING', (0, 0), (-1, -1), 0),
            ('TOPPADDING', (0, 0), (-1, -1), 0),
            ('BOTTOMPADDING', (0, 0), (-1, -1), 0),
            ('VALIGN', (0, 0), (-1, -1), 'TOP'),
            ('SPAN', (0, 0), (0, -1)),
        ]))
        self._elements.append(KeepTogether([box, Spacer(1, 3 * mm)]))

    # ── Numbered bullet list ─────────────────────────────────────
    def numbered_list(self, items: list[str]):
        """Numbered steps with blue circle badges, like the auth flow."""
        for i, item in enumerate(items, 1):
            badge = Table(
                [[Paragraph(str(i), self.s_bullet)]],
                colWidths=[20], rowHeights=[20],
            )
            badge.setStyle(TableStyle([
                ('BACKGROUND', (0, 0), (-1, -1), BLUE),
                ('ROUNDEDCORNERS', [10, 10, 10, 10]),
                ('VALIGN', (0, 0), (-1, -1), 'MIDDLE'),
                ('ALIGN', (0, 0), (-1, -1), 'CENTER'),
            ]))
            row = Table(
                [[badge, Paragraph(item, self.s_bullet_text)]],
                colWidths=[28, USABLE - 28],
            )
            row.setStyle(TableStyle([
                ('VALIGN', (0, 0), (-1, -1), 'MIDDLE'),
                ('LEFTPADDING', (0, 0), (-1, -1), 0),
                ('TOPPADDING', (0, 0), (-1, -1), 2),
                ('BOTTOMPADDING', (0, 0), (-1, -1), 2),
            ]))
            self._elements.append(row)
        self._elements.append(Spacer(1, 3 * mm))

    # ── Bullet list (simple) ────────────────────────────────────
    def bullet_list(self, items: list[str], bold_prefix: bool = False):
        """Simple bullet list with • prefix."""
        for item in items:
            t = f'•&nbsp;&nbsp;{item}'
            self._elements.append(Paragraph(t, self.s_body))

    # ── Response fields table ────────────────────────────────────
    def response_table(self, rows: list[tuple]):
        """Response fields: (Campo, Tipo, Descricao)."""
        self.param_table(
            rows,
            headers=('Campo', 'Tipo', 'Descricao'),
            col_widths=[110, 55, USABLE - 165],
        )

    # ── Spacer / PageBreak ───────────────────────────────────────
    def spacer(self, height_mm: float = 3):
        self._elements.append(Spacer(1, height_mm * mm))

    def page_break(self):
        self._elements.append(PageBreak())

    # ── Build ────────────────────────────────────────────────────
    def build(self):
        """Generate the PDF file."""
        os.makedirs(os.path.dirname(os.path.abspath(self._output)), exist_ok=True)

        doc = SimpleDocTemplate(
            self._output,
            pagesize=A4,
            leftMargin=LEFT,
            rightMargin=W - RIGHT,
            topMargin=30,  # space for header
            bottomMargin=55,  # space for footer
        )

        # Prepend cover
        final = []
        self._cover()
        final = self._elements

        topic = self._topic
        date_str = self._date

        def canvas_maker(fn, **kwargs):
            c = _NumberedCanvas(fn, topic=topic, date_str=date_str, **kwargs)
            return c

        doc.build(final, canvasmaker=canvas_maker)
        size_kb = os.path.getsize(self._output) / 1024
        print(f'PDF generated: {self._output}')
        print(f'Size: {size_kb:.1f} KB')
