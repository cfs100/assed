CREATE TABLE shows (
  id integer primary key,
  name string not null,
  last integer null
);

CREATE TABLE episodes (
  id integer primary key,
  show integer not null,
  name string not null,
  magnet string not null,
  date integer not null
);

CREATE TABLE ignored (
  name string primary key,
  date integer not null
);
