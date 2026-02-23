---
description: >-
  This agent is an expert of this app called BrickScanr.
  Its an app that handles user search and request for LEGO bricks and sets. We make external requests to API
  clients to get data. We use runtime and websockets with our frontend to handle heavy fetching.
  Every item has core data and locale data. Every request we receive is locale dependent.
  The same request but for different locales is likely to have different results.
tools: ['insert_edit_into_file', 'replace_string_in_file', 'create_file', 'run_in_terminal', 'get_terminal_output', 'get_errors', 'show_content', 'open_file', 'list_dir', 'read_file', 'file_search', 'grep_search', 'validate_cves', 'run_subagent', 'semantic_search']
---
The app is written in GoLand with a Redis database.
The frontend is written with Angular as framework.

You can find the main.go file in the root of the project.

All other code can be found in the internal folder.

API communication related to the frontend:
- routing is in internal/router folder.
- http handlers are in the internal/handlers folder.
- middlewares are in the internal/middlewares folder. More specifically the locale middleware which is necessary to pass
on the locale to the APi clients while fetching our data.

We do not own the data, we use external platforms that own this data. Therefore, we ought to be very respectful towards
those platforms. These are the API clients we use :
- bricklink at internal/bricklink : submit a search query, fetch a set inventory, get brick summary
- lego at internal/lego : get set details
- pickabrick at internal/pickabrick : get brick details

Every time we request data from those API clients, we must check for errors and differentiate between 2 types:
- standard : there was a problem with the request (network, timeout, etc.)
- not found : body is empty, list is empty, etc.

Performance wise, we have a caching system in place, so if we request data for a set or a brick, we first check if we
have it in the cache, if not we request it to the API and then we store it in the cache for future requests.
This is very useful since most of the API calls we make are single target for a single item, so very uneffective.

To improve the fetching time, we have instilled a worker pool system that is dynamically adapting to the request and
also depending on the load of our system.
You can find this at internal/workerpool folder.

To avoid being hit by rate limits, we have a queue system in place with delays, backoff, retry and that can be adaptive.
You can find this at internal/throttle folder.

To represent LEGO items, there is the following :
- brick at internal/brick
- set at internal/set

They have different variations. Those variations allow to send and cache precisely the data we need. Examples : 
- Core struct that can be shared
- Locale struct that contains locale specific details
- Brick Inventory struct that contains details about the inventory of a set, like quantity, price, etc.
- Set External struct that contains details about the set that are only useful for the API clients and runtime.

When fetching a set, we use a runtime with a websocket to send progressive updates while fetching.
It handles multiple runtime sets, each is related to a specific set ID and locale.
Multiple sets can be fetched at the same time, each has its own runtime.
This is in the internal/setruntime folder.

- to interact from the http handlers : internal/setruntime/handler.go
- the main logic of the runtime : internal/setruntime/runtime.go

The main thread is a big for select loop that listens to multiple channels, like data changes, new connections, disconnections, packets from the frontend, etc.
You can find it in the internal/setruntime/runtime.go file, in the `run` function.

The setruntime communicates with the frontend with websockets, so each change in the runtime is directly sent to the frontend.

It works using data and packets that are available at :
- internal/setruntime/data.go
- internal/setruntime/packets.go
- internal/setruntime/progress.go, while fetching the inventory and in order to have smooth updates

When there is an update, the runtime is updated with the new data and the runtime is
listing to a channel for data changes, the dataChange struct is defined hereby:

```go
type dataChange struct {
    Id       uuid.UUID
    Type     DataType
    Reason   DataChangeReason
    Progress Progress // Only used when working with batches
}
```

And the code to push an event is, for example here the update of a set:
```go
    h.PushChange(runtimeID, setID, DataTypeSet, DataTypeUpdated)
```
(h is the handler that contains all runtimes)

To ensure the set's data integrity, we use the set handler to manage the changes : internal/setruntime/set_handler.go

Similarly for bricks, we have the bricks handler : internal/setruntime/bricks_handler.go

Finally, before starting a new runtime set, we look for the set's data in the cache using internal/setruntime/cache.go.

We built our app ready to handle various databases but are only using Redis for now. We do not own data but want to
avoid spamming the API clients. We have health monitoring, TTLS, locks etc. in internal/database. For more redis
specific code, you can find it in internal/redis which we use for every query we make.

Actual redis keys for references:
- brick:{elementID}:{locale}: for specific bricks details
- brick:design:{designID}:{locale}: brick details + to look at underlying bricks under the same designID
- set:bricklink:{bricklinkID}: to link a bricklinkID to a set uuid (see keys below under format set:{uuid})
- set:slug:{slug}: to link a slug to a set uuid (see keys below under format set:{uuid})
- set:{uuid}: for generic set details that can be shared between all locales
- set:{uuid}:{locale}: locale specific details only

For backend translations we're using a library called lingo (through config files in config/translations/ folder)
For the exports we're using a library called go-spit (creation of excel and csv files)

## Agent expectations

- Prefer existing patterns over inventing new ones
- When unsure, ask before changing runtime logic
- Always ensure that changes do not disrupt real-time runtime operations
- When a change is made on models that are used in the runtime, ensure that the runtime is updated accordingly
- Make sure the tests are running, and add or edit tests when you add new features or change existing ones
- When changing anything related to swagger (models, endpoints, etc.) always update the swagger documentation accordingly (swag init -ot yaml)
- Please dont check if swagger.json or swagger.yaml we're changed in the right way, i will do that myself
- At the end of all your changes, i want you to write me a prompt that i can use to edit the frontend to match the backend changes you made (angular front)