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
Each new file uses one fast download from the account's allowance for a rolling window; downloading a recent file again is free. Downloads report downloads_left; account_status shows the allowance, so check it before downloading many files.
Downloads write under ANNAS_DOWNLOAD_PATH (by default annas-archive in the user's home folder) and never overwrite existing files. They return path and bytes; tell the user where the file was saved.
Errors begin with a stable code: INVALID_ARGUMENT, CONFIG, NOT_FOUND, UPSTREAM_BLOCKED, REQUEST_TIMEOUT or UPSTREAM.
Report errors; do not claim a file was downloaded unless the tool succeeded.
When an error says sign-in failed or mentions ANNAS_ACCOUNT_COOKIE, give the user its steps for setting the cookie by hand.
