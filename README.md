Iris-message-processor
========

Iris-message-processor is a fully distributed Go application meant to replace the sender functionality of [Iris](https://github.com/linkedin/iris/tree/experimental) and provide reliable, scalable, and extensible incident and out of band message processing and sending.

Iris-message-processor is meant to be used with the [experimental-sender branch](https://github.com/linkedin/iris/tree/experimental) of Iris. Make sure you have set that up first before running Iris-message-processor.


Set up database
--------------

Note: this is a separate database than the one used by Iris

1. create mysql database and schema: `mysql -u USER -p < iris-message-processor/schemas/schema.sql`  (WARNING: This will drop any existing tables)
2. create mysql user: `CREATE USER 'iris_message_processor'@'localhost' IDENTIFIED BY '1234';`
3. grant new user permissions: `GRANT ALL PRIVILEGES ON 'iris_message_processor'.* TO 'iris_message_processor'@'localhost';`


Set up configurations
--------------
The application configurations are defined in iris-message-processor/config/cfg.json. You will need to modify this file with your specific mysql and Iris settings.

### Mysql credentials:
1. Make sure that these configs match with the database/user you are using

- "MySQLUser":"iris_message_processor",
- "MySQLPassword":"1234",
- "MySQLAddress":"127.0.0.1:3306",
- "MySQLDBName":"iris_message_processor",

### Iris configurations:
1. Make sure you have first configured and run an instance of Iris from the [experimental-sender branch](https://github.com/linkedin/iris/tree/experimental)
1. create a new application "iris-message-processor" and copy the API key into the "IrisKey" configuration

- "IrisBaseURL":"http://127.0.0.1:16649",
- "IrisAppName":"iris-message-processor",
- "IrisKey":"KEY_FOO",

### Debug/testing settings:

1. By default the configs are set up with dryrun and debug settings enabled, you will need to disable these in production but they can be used for testing

- "MetricsDryRun": true, // with this setting enabled IMP will skip emitting metrics
- "SenderDryRun":true, // with this setting enabled messages will be sent with the dummyVendor that just logs/stores the messages but does not actually send them
- "IrisDryRun":true, // with this setting enabled IMP will not advance the steps of an iris incident
- "StorageDryRun":true, // with this setting enabled IMP will not persist messages to the DB
- "DebugAuth": true, // with this setting enabled IMP will not enforce HMAC authentication for its API
- "DebugLogging": true // with this setting enabled IMP will set the logging level to debug

Run
--------------
In the iris-message-processor directory:
```bash
go run .
```

Tests
-----

Run tests:

In the iris-message-processor directory:
```bash
go test -v ./...
```

API
-----

The Iris-message-processor API is mainly used for interfacing with Iris but also provides a general health checking endpoint for monitoring a running instance of Iris-message-processor. With the exception of the "admin" endpoint the API should not be interacted with directly, instead clients should query Iris's API which will reach out to Iris-message-processor when needed.

### Endpoints:

#### /admin

This endpoint is used for health checking. It returns "GOOD" if the API is ready to serve requests and "BAD" otherwise.
