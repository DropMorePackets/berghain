# Berghain Design Doc

Berghain has two central structures on which the validation and verification works.

Every Request starts by being loaded into an Identity containing the SrcAddr, Host, Frontend and Level. This Identity
can be used to ask the validator if the given cookie is valid.

If it is not valid, a response is sent to indicate that the client should be redirected to berghain itself. Berghain
exposes a http server handling all the logic required to create a new cookie for the user.

## Hetzner

https://accounts.hetzner.com/_ray/pow

Types:

1. Check if cookie gets set
2. slowdown with js sleep, value set by cookie

Cookie

    heray-clearance=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiI5NmNkMmRmZS1kYjRhLTRkMDUtODkxZi0xMjU5Mjc1N2Q4M2UifQ.lZpSBjKXFZFJcssyHZGi_msS0O3sj-q4mBJJ8KyhzjY; PHPSESSID=bbbf6067fd3fb1236a079a30858f8e01

< set-cookie:
heray-clearance=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiJhZjllNmNkMC03YzQzLTQzMjktYjQxNy1jN2QyZTlmNmJkMWIifQ.4M3tnPkQCbec4o4oCYu9EuLRRnD48n6cnL61wsZj0YA;
Domain=accounts.hetzner.com; Path=/; Secure; HttpOnly; SameSite=Strict
{
"alg": "HS256",
"typ": "JWT"
}
{
"uid": "af9e6cd0-7c43-4329-b417-c7d2e9f6bd1b"
}

< set-cookie:
heray-user-session=P8Q1Q3egBWhufKTNu-9jRQ|1695692792|5JP4YXgr6SGrIjpYLgb5KrzE3dozcRZQlYucH1DSeqMacoALweYogJ7g0xMutjwwRHuZazpoem01oPy4V-hGDQ|BJQic3cIHqIwkIUexiH2Vyrp1sk;
Path=/; SameSite=Lax; Secure; HttpOnly
unknown|time|unknown|unknown

< set-cookie: HERay_WaitFor=62; Domain=accounts.hetzner.com; Path=/; SameSite=Strict

## Babiel

https://babiel.com/.enodia/challenge

Types:

1. Cookie with POW Challenge

Runtime:

1. Website contains first challenge
2. Send for validation
3. Check for new Challenge

Cookie

    enodia=eyJleHAiOjE2OTI4MzUzMzEsImNvbnRlbnQiOnRydWUsImF1ZCI6ImF1dGgiLCJIb3N0Ijoid3d3LmJ1bmRlc3RhZy5kZSIsIlNvdXJjZUlQIjoiOTEuMC4yOS40MiIsIkNvbmZpZ0lEIjoiOGRhZGNlMTI1ZmQyYzM5MzJiOTQzYjUyZTlkMmNkNjUwNTc1NGUxNjIyMTJhMmNlMWJiNWFmMTVjMGQ0YmJmZSJ9.knObOtKZgLPnFIEEW9AYq2nAAeTFqk295D0mqteZ8uA=

## Cloudflare

https://challenges.cloudflare.com/turnstile/v0/g/313d8a27/api.js?onload=URXdVe4&render=explicit

https://challenges.cloudflare.com/cdn-cgi/challenge-platform/h/g/orchestrate/chl_api/v1?ray=7fb72b3cba9bc4a4


##  HAProxy-Protection

https://gitgud.io/fatchan/haproxy-protection/-/blob/a6f3613b6a4e41860f4916de508de80e47e2ee98/src/js/worker.js