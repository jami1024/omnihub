-- 0031_user_email_login: make portal login email-first.
--
-- New signups use email as their login identity. Existing rows keep
-- working at the data level; rows that already have email get a
-- case-insensitive uniqueness guarantee.

UPDATE users
   SET email = username
 WHERE (email IS NULL OR email = '')
   AND username LIKE '%@%';

CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_unique
    ON users (lower(email))
 WHERE email IS NOT NULL AND email <> '';
