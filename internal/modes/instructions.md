Use these tools when the user needs the full text of a specific book, paper, standard, or citation.
Search before downloading and compare title, authors, language, and format.
article_search accepts bare DOIs, doi: identifiers, DOI URLs, or keywords.
Keywords search journal articles; book_search searches books.
Pass the selected 32-character hash to a download tool, or a DOI to article_download.
Download format only names the extension; it does not convert files.
Search returns at most 10 hits by default; matched counts the parsed hits on that page.
Raise limit before increasing page.
Downloads write under ANNAS_DOWNLOAD_PATH, preserve existing files, and return path and bytes.
Errors begin with a stable code: INVALID_ARGUMENT, CONFIG, NOT_FOUND, UPSTREAM_BLOCKED, REQUEST_TIMEOUT, or UPSTREAM.
Report errors; do not claim a file was downloaded unless the tool succeeded.
