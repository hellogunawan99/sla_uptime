untuk ini pakai async_mysql, summary_uptime, summary_downtime

3 directory ini

1. async_mysql untuk ambil data update dari mysql dan continuously update terus tiap 5 detik
2. summary_uptime untuk insert ke table summary_uptime sebagai record perjam dengan percentage uptime (sudah dikurangin dengan kondisi pekerjaan schedule)
3. summary_downtime untuk isert ke table summary_downtime sebagai record apa saja pekerjaan yang menyebabkan downtime schedule (tidak mempengaruhi sumamry_uptime) tidak jadi pakai karna pakai store procedure di mysql
