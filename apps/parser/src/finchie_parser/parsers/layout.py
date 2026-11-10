"""Reading a page by position: words into printed rows, and words under the column headings they line up
with. Banks align columns differently (right-aligned amounts, centred headings), so each parser says which
way its own do."""

from collections.abc import Iterable, Mapping

from finchie_parser.pdf import Word


def rows_of(words: Iterable[Word], tolerance: float = 2.0) -> list[list[Word]]:
    """Words grouped into printed rows, top to bottom, each left to right: words whose tops are within
    `tolerance` points of a row's first word belong to it."""
    rows: list[list[Word]] = []
    for word in sorted(words, key=lambda w: (w.top, w.x0)):
        if rows and abs(rows[-1][0].top - word.top) <= tolerance:
            rows[-1].append(word)
        else:
            rows.append([word])
    return [sorted(row, key=lambda w: w.x0) for row in rows]


def nearest_column(word: Word, headings: Mapping[str, Word]) -> str:
    """The heading whose right edge is nearest the word's: amounts are printed right-aligned."""
    return min(headings, key=lambda name: abs(headings[name].x1 - word.x1))


def nearest_centre(word: Word, headings: Mapping[str, Word]) -> str:
    """The heading whose centre is nearest the word's: for headings centred over their columns."""
    return min(headings, key=lambda name: abs(_centre(headings[name]) - _centre(word)))


def _centre(word: Word) -> float:
    return (word.x0 + word.x1) / 2
