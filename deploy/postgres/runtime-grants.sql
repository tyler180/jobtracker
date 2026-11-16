-- Run as the database/schema owner after explicitly initializing schema v2.
-- Create the jobtracker_app LOGIN role and set its password through your secret
-- management process first. Do not give this role schema ownership/superuser.
GRANT USAGE ON SCHEMA jobtracker TO jobtracker_app;
GRANT SELECT ON jobtracker.schema_versions TO jobtracker_app;
GRANT SELECT, INSERT ON jobtracker.users TO jobtracker_app;
GRANT SELECT, INSERT, DELETE ON jobtracker.jobs TO jobtracker_app;
GRANT UPDATE (document) ON jobtracker.jobs TO jobtracker_app;
GRANT SELECT, INSERT, DELETE ON jobtracker.sessions, jobtracker.login_attempts TO jobtracker_app;
