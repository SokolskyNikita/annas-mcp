Use these tools when the user needs the full text of a specific book, paper, standard or citation, or details about an edition.
Search before downloading and compare title, authors, year, language and format.
Narrow searches with filters rather than paging: format (such as epub), language, content, source, title, author and year. Set downloadable_only when you mean to download.
Prefer hits with fast_download true; book_download cannot fetch the others. Avoid hits with possible_issues when another copy fits.
article_search finds journal articles by keyword, or one article by bare DOI, doi: identifier or DOI URL. book_search searches books; metadata_search searches library catalogs, including books with no file, for ISBNs and editions.
results honor every filter. partial_matches are looser hits Anna's Archive adds to a short first page and may ignore filters; check them before use.
Search returns at most 10 hits by default. matched counts the exact hits on that page, total counts all of them and more_pages says whether another page exists.
Raise limit before increasing page.
book_details takes a hash or a metadata_search id and returns identifiers such as ISBN and DOI, the edition, description, subjects and problems reported for the file. Use it to confirm an edition or cite a work.
Pass the selected 32-character hash to a download tool, or a DOI to article_download.
Download format only names the extension; it does not convert files.
Downloads check completed files directly under ANNAS_DOWNLOAD_PATH (by default annas-archive in the user's home folder) against the full MD5. A match returns the existing path and bytes regardless of filename or extension, without downloading again; downloads_left is omitted. article_download resolves a DOI to its hash before checking.
If no file matches, downloads save a verified copy using a unique name and never overwrite an existing file. A fresh fast download may use the account's rolling-window allowance. Downloads return path and bytes, and downloads_left when available after a fast download; account_status shows the allowance. Tell the user the returned file path.
Errors begin with a stable code: INVALID_ARGUMENT, CONFIG, NOT_FOUND, UPSTREAM_BLOCKED, REQUEST_TIMEOUT or UPSTREAM.
Report errors; do not claim a file was downloaded unless the tool succeeded.
When an error says sign-in failed or mentions ANNAS_ACCOUNT_COOKIE, give the user its steps for setting the cookie by hand.
