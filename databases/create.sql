CREATE TABLE IF NOT EXISTS shows (
  id integer primary key,
  name string not null,
  last integer null
);

CREATE TABLE IF NOT EXISTS episodes (
  id integer primary key,
  show integer not null,
  name string not null,
  magnet string not null,
  date integer not null
);

CREATE TABLE IF NOT EXISTS mismatch (
  name string not null,
  show integer not null,
  date integer not null
);

CREATE TABLE IF NOT EXISTS ignored (
  name string primary key,
  date integer not null
);
