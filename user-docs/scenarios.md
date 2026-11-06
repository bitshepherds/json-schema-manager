# Schema Family Scenarios <!-- omit in toc -->

- [What JSM does today](#what-jsm-does-today)
- [Words used on this page](#words-used-on-this-page)
- [The four facts](#the-four-facts)
- [The scenarios](#the-scenarios)
  - [1. Provider moves first](#1-provider-moves-first)
  - [2. Consumer moves first](#2-consumer-moves-first)
  - [3. Config files](#3-config-files)
  - [4. Stored data, one schema for all](#4-stored-data-one-schema-for-all)
  - [5. Stored data, each document names its version](#5-stored-data-each-document-names-its-version)
  - [6. Retained streams](#6-retained-streams)
  - [7. Uncoordinated parties](#7-uncoordinated-parties)
  - [8. Lock-step](#8-lock-step)
  - [9. A format someone else owns](#9-a-format-someone-else-owns)
- [Declaring a family's scenario](#declaring-a-familys-scenario)
- [The rule](#the-rule)
- [How long a version lasts](#how-long-a-version-lasts)
- [What a major bump means](#what-a-major-bump-means)
- [Deliberate exceptions](#deliberate-exceptions)
  - [The way out of a red deployed version](#the-way-out-of-a-red-deployed-version)
- [The promises](#the-promises)
- [What the checks can and cannot show](#what-the-checks-can-and-cannot-show)
- [How to choose a scenario](#how-to-choose-a-scenario)
- [Terms from other schema registries](#terms-from-other-schema-registries)

A schema family's versions change over time, and whether a change is safe depends on how the family is used. Adding a value to a list of allowed order statuses breaks a service that receives order events and has not upgraded yet, but adding an option to a settings file harms nobody when a newer tool reads the file. Making a field required is safe for that same service, but in a database it locks out every record saved before the field existed.

This page describes the common ways schemas are used, what can go wrong in each, and what JSM checks for each. Each family serves one of these scenarios.

## What JSM does today

- Every family [declares its scenario](#declaring-a-familys-scenario), and the declaration decides which documents run across versions, within a major, as [the rule](#the-rule) says. `validate` and `build-dist` run the same rule, and `validate --watch` runs it for what changed. Patch and minor are treated alike, and no document crosses a major.
- The same pairs of versions run however a version is selected: by key, by search scope or with `all`. Validating an earlier version runs its pairs with later versions too, so it can't miss a later breakage. A failure appears under the later version, since that's what changed.
- A new version carries only the documents for what it changes, so documents aren't copied on a patch or minor bump, except in a family with no pairs of versions to run. `--strict` counts inherited documents. See [What a version carries](./developing-schemas.md#what-a-version-carries).
- `validate` compares the same pairs of versions structurally, and warns about each change in a direction the family protects which no document shows. A writer-ahead family needs a `pass` document for a change that widens what's accepted, a reader-ahead family a `fail` document for one that narrows it, an either family both, and a neither family none. See [Unexercised changes](./developing-schemas.md#unexercised-changes). `--strict` makes every warning a failure, in `validate` and `build-dist`.
- A version can list a [deliberate exception](#deliberate-exceptions) to the rule, with a reason, for one run of one document. `validate`, `build-dist` and `check-party` then don't fail on that run, and nothing else is cleared.
- `check-party` tells a reader or writer whether later versions in its major can harm it, following the family's declaration and the role its manifest states. It runs the same pairs as `validate`, for that side of the rule, and honours the same exceptions. Where no later version can harm the party, such as a reader in a reader-ahead family, it says why and exits `0`. See [Checking a party's schema versions](./developing-schemas.md#checking-a-partys-schema-versions).
- What happens to old versions follows [how long the family's documents live](#how-long-a-version-lasts). `check-changes` refuses deleting a deployed version whose documents may still exist, and a retention family's `oldestLive` takes older versions out of the checks. A new major in a family whose old documents stay around needs [a migration statement](#what-a-major-bump-means).

## Words used on this page

- **Writer** and **reader**: whoever produces a document, and whoever validates it. The rest of these docs often say provider and consumer, for the same roles in a message flow. A writer can also be a person editing a file, or a service that saved a record years ago.
- **Ahead**: on a newer version of the schema than the other side. In a major version, a writer on `1.3.0` is ahead of a reader on `1.1.0`.
- **Loosening**: a later version accepts something an earlier one rejected. Raising a `maxLength` from 10 to 20 is a loosening.
- **Tightening**: a later version rejects something an earlier one accepted. Lowering that `maxLength` from 20 to 10 is a tightening.

## The four facts

Four facts about a family decide what can break it.

1. **Who can be ahead?** Which versions can meet: a writer on a newer version than its reader, a reader on a newer version than its writer, either, or neither.
2. **How long do documents outlive their writer?** A moment, a retention period, or indefinitely. This sets how far back the checks reach, whether a major bump needs migration, and whether a version may ever be deleted.
3. **Does the reader pick the schema from the document?** If each document names its version and is validated against that version, versions needn't be compatible with each other.
4. **Does the schema prescribe or describe?** A prescriptive schema sets the rules, and you choose when they change. A descriptive schema records a format someone else owns, and they change it without asking.

## The scenarios

Nine scenarios name the common combinations of those facts.

| # | Scenario | Example | Who can be ahead | Documents live |
| --- | --- | --- | --- | --- |
| 1 | [Provider moves first](#1-provider-moves-first) | Events from a service, API responses, a CLI's JSON output | Writer | A moment |
| 2 | [Consumer moves first](#2-consumer-moves-first) | API request bodies, partners submitting to one service | Reader | A moment |
| 3 | [Config files](#3-config-files) | A tool's settings file | Reader | Indefinitely |
| 4 | [Stored data, one schema for all](#4-stored-data-one-schema-for-all) | A database of mixed versions, validated on read | Reader | Indefinitely |
| 5 | [Stored data, each document names its version](#5-stored-data-each-document-names-its-version) | Documents carrying a version tag | Neither, for validation | Indefinitely |
| 6 | [Retained streams](#6-retained-streams) | Replayable event logs, dead-letter queues | Either | A retention period |
| 7 | [Uncoordinated parties](#7-uncoordinated-parties) | Several providers on a bus, syncing mobile clients, a published standard | Either | Varies: the family states it |
| 8 | [Lock-step](#8-lock-step) | Both sides released together | Neither | A moment |
| 9 | [A format someone else owns](#9-a-format-someone-else-owns) | A third party's API response or file format | Reader, catching up | Varies: the family states it |

An API operation is both scenario 1 and scenario 2, so its request and its response are separate families.

Each scenario below ends with what JSM checks for it. [The rule](#the-rule) says what each check proves.

### 1. Provider moves first

A distributed system evolves component by component: a lagging consumer on the same major still accepts the new messages from an updated provider. The schema must stay open, or nothing can be added in a minor. Making a field required is safe, because it strengthens what consumers are promised.

**Worked example: an `order-placed` event.**

- **Who writes:** the Orders service, which publishes each event to a message bus.
- **Who reads:** the Billing and Shipping services, each released on its own timetable, and each validating against the version it was built with.
- **The change:** version `1.1.0` adds `"refunded"` to the allowed values of `status`.
- **What breaks without the check:** Orders deploys, and sends its first refunded order. Billing, still on `1.0.0`, rejects the event, so a refund goes unprocessed.
- **What stays safe:** adding an optional `giftMessage` property, since an open `1.0.0` already accepts it. Making `customerId` required, since every event Billing now receives has what it already allowed.

**What JSM checks:** a writer-ahead family needs no loosening.

- Later `pass` documents run on earlier versions, so a `1.1.0` `pass` document with `"status": "refunded"` fails on `1.0.0`.
- Earlier `fail` documents run on later versions, so a `1.0.0` `fail` document with `"status": "refunded"` catches the change even when no `1.1.0` document uses the value. If no `pass` document uses the new value, `validate` warns too.

### 2. Consumer moves first

Old clients' documents still pass, so new rules can't apply to them within a major.

**Worked example: a `create-refund` API request body.**

- **Who writes:** partners' systems, which each integrate with the API and change on their own timetables.
- **Who reads:** the Payments service, which deploys a new version of the schema before any partner adopts it.
- **The change:** version `1.1.0` makes `reason` required.
- **What breaks without the check:** Payments deploys, and every partner's request that worked yesterday is refused, because none of them sends `reason` yet.
- **What stays safe:** adding `"store-credit"` to the allowed values of `refundMethod`, or adding an optional property to a closed schema. Partners who don't use them are unaffected, and Payments is ready for those who do.

**What JSM checks:** a reader-ahead family needs no tightening.

- Earlier `pass` documents run on later versions, so a `1.0.0` `pass` document with no `reason` fails on `1.1.0`. Later `fail` documents run on earlier versions, so a `1.1.0` `fail` document with no `reason` fails on `1.0.0`, which accepts it.
- Later `pass` documents don't run on earlier versions, so the harmless `"store-credit"` change passes, even though `1.0.0` rejects a `pass` document that uses it.

### 3. Config files

Old files load after a tool upgrade. Schemas are often closed to catch typos, so a new option is an allowed loosening.

**Worked example: `deployer.yml`, the settings file of a deployment tool.**

- **Who writes:** the people in each team using the tool, who edit the file by hand and keep it in their repositories for years.
- **Who reads:** the tool, on whichever release each team has upgraded to. The schema is closed with `"additionalProperties": false`, so a mistyped option is an error rather than silently ignored.
- **The change:** version `1.1.0` renames the option `timeout` to `timeoutSeconds`.
- **What breaks without the check:** a team upgrades the tool, and a settings file that has worked for three years no longer loads.
- **What stays safe:** adding a new option, `parallel`. A file using it would be rejected by `1.0.0`, but that needs an older tool reading a newer file, and this check doesn't promise that. A `fail` document with the typo `paralel` should still be rejected by both versions.

**What JSM checks:** a reader-ahead family needs no tightening.

- Earlier `pass` documents run on later versions, so a `1.0.0` `pass` document that uses `timeout` fails on `1.1.0`. Later `fail` documents run on earlier versions.
- Later `pass` documents don't run on earlier versions, so the harmless `parallel` option passes.
- `check-changes` refuses deleting a deployed version, since files written against it may live indefinitely, and `validate` requires [a migration statement](#what-a-major-bump-means) for each new major.

### 4. Stored data, one schema for all

Every old document stays valid, however old. A major bump doesn't make old data vanish, so it needs migration.

**Worked example: customer profiles in a document database.**

- **Who writes:** every release of the Accounts service there has ever been, saving profiles over many years. The profiles carry no version.
- **Who reads:** the current release of the Accounts service, which validates each profile against the newest schema as it loads it.
- **The change:** version `1.4.0` makes `email` required.
- **What breaks without the check:** a customer who signed up in 2019, before the service collected email addresses, can no longer log in, because their profile is rejected as it loads.
- **What stays safe:** adding an optional `preferredLanguage` property. Old profiles without it still pass.
- **A major bump:** moving `email` to required in `2.0.0` doesn't solve this. The 2019 profile is still in the database, and the Accounts service must migrate it, with code you write, before `2.0.0` can read it.

**What JSM checks:** a reader-ahead family needs no tightening.

- Earlier `pass` documents run on later versions, so a `1.3.0` `pass` document with no `email` fails on `1.4.0`, and on every later version in the major. Later `fail` documents run on earlier versions.
- Later `pass` documents don't run on earlier versions, so adding an option the old versions reject passes.
- `check-changes` refuses deleting a deployed version, and `validate` requires [a migration statement](#what-a-major-bump-means) for each new major. JSM records the statement. The migration is your code, and JSM tests no data.

### 5. Stored data, each document names its version

Documents are a mix of versions, each validated against its own. No version may ever be deleted, and the reader or migration code must handle each one.

**Worked example: an archive of issued invoices.**

- **Who writes:** the Invoicing service, which adds `"schemaVersion": "1.2.0"` to each invoice as it issues it. Invoices are never changed afterwards.
- **Who reads:** the audit tool, which reads the version from each invoice and validates the invoice against that version.
- **The change:** version `1.3.0` makes `taxNumber` required, and a tidy-up deletes version `1.2.0` from the registry, since nothing issues invoices with it any more.
- **What breaks without the check:** the audit tool meets an invoice from 2022 that names `1.2.0`, and has no schema to validate it with. Making `taxNumber` required harms nothing: no `1.2.0` invoice is ever validated against `1.3.0`.

**What JSM checks:** a family whose reader picks the schema from the document behaves as `neither`, so only each version's own documents run.

- Only each version's own documents run, since these versions never need to agree.
- `check-changes` refuses deleting a deployed version.
- A new major needs no migration statement, because old documents keep naming the version they were written under.

### 6. Retained streams

Scenario 1, plus new consumers reading old events. Retention can limit how far back the checks go.

**Worked example: a `payment-taken` event log kept for 30 days.**

- **Who writes:** the Payments service.
- **Who reads:** the Fraud service, which may lag behind the writer on an older version, and a new Analytics service, built against the newest version, which replays the log from its oldest event.
- **The change:** version `1.3.0` makes `currency` required. Scenario 1 would allow this, and Fraud is unaffected.
- **What breaks without the check:** Analytics starts up and replays events written three weeks ago under `1.2.0`. Those events have no `currency`, so Analytics rejects them. Adding a value to an enum breaks the other side: Fraud rejects the new value, as in scenario 1.
- **What retention changes:** once 30 days have passed since the last `1.0.0` event was written, no such event remains, so `1.0.0` no longer constrains later versions.

**What JSM checks:** an either family needs no loosening and no tightening.

- All four runs across versions take place, so a `1.2.0` `pass` document with no `currency` fails on `1.3.0`, and so does an enum value added in `1.3.0` when a document shows it.
- The family declares its oldest live version, `oldestLive`, and moves it forward by a reviewed change. [Older versions then take no part in the checks](#how-long-a-version-lasts), and `check-changes` allows deleting them. JSM uses no clock, so results are repeatable.

### 7. Uncoordinated parties

Nobody controls upgrade order, so few changes are safe in a minor.

**Worked example: notes synced between mobile app installs.**

- **Who writes:** each install of the app, on whichever release its owner has updated to.
- **Who reads:** every other install on the same account, also on whichever release its owner has updated to.
- **The change:** version `1.1.0` adds `"pinned"` to the allowed values of `state`, and makes `updatedAt` required.
- **What breaks without the check:** both directions fail. A phone on the new release syncs a pinned note to a tablet on the old one, which rejects it. The tablet syncs a note with no `updatedAt` to the phone, which rejects that too.
- **What stays safe:** a new description, and little else. See [why "either" is only as strong as its samples](#either-is-only-as-strong-as-its-samples).

**What JSM checks:** an either family needs no loosening and no tightening.

- All four runs across versions take place, so a `1.0.0` `pass` document with no `updatedAt` fails on `1.1.0`, and so does a `1.1.0` `pass` document with `"state": "pinned"` on `1.0.0`.

### 8. Lock-step

Only each version's own documents matter, and cross-version checks would be noise.

**Worked example: a plan file passed between two steps of a batch job.**

- **Who writes:** the job's Planner step, which writes `plan.json`.
- **Who reads:** the job's Executor step, which reads `plan.json` and discards it. Both steps ship in one container image, so they are always on the same version.
- **The change:** version `1.1.0` renames `steps` to `tasks`. No reader on `1.0.0` can ever meet a `1.1.0` document, so this harms nobody.
- **What breaks without the check:** the check that matters here is each version against its own documents. If `1.1.0` requires a misspelt `taks`, it rejects every real plan, and its own `pass` document shows that before the job fails in production.

**What JSM checks:** a neither family runs only each version's own documents.

- Only each version's own documents run, so the rename passes.

### 9. A format someone else owns

A new version records a change that has already happened. A failed check means "the real format has changed, and here's how", not "you made a mistake, don't ship". Test documents are captured samples, and old samples usually still need to pass. Choosing a major bump changes nothing, because the documents already exist.

**Worked example: a shipping carrier's tracking webhook.**

- **Who writes:** the carrier, a separate company, which changes the format when it chooses and may not say so.
- **Who reads:** your Tracking service, which receives each webhook and validates it. Its schema describes what the carrier sends.
- **The change:** the carrier starts sending `"status": "held-at-customs"`. You capture a real message as a `pass` document for version `1.1.0`, and add the value to the schema.
- **What breaks without the check:** while drafting `1.1.0`, you rewrite the `status` list from recent samples and drop `"out-for-delivery"`, which they happen not to show. The carrier still sends it, and the Tracking service rejects every parcel on its last leg. A captured `1.0.0` sample with that status would have shown this.

**What JSM checks:** this family follows reader-ahead. The schema is descriptive, which changes the wording of a failure but not which documents run.

- Earlier `pass` documents run on later versions, so a captured `1.0.0` sample that uses `"out-for-delivery"` fails on `1.1.0`, and the failure says the real format has changed rather than that you made a mistake. Later `fail` documents run on earlier versions.
- Later `pass` documents don't run on earlier versions, so the new sample raises no alarm: `1.0.0` rejecting it tells you only what you already know.

## Declaring a family's scenario

A family names its scenario in a required file, `family.yml`, in the family's directory, beside its version directories. `jsm create-schema` asks for the scenario, or takes it as `--scenario`, and writes the file:

```yaml
scenario: provider-first
```

The scenario implies four facts, and the file may override any of them:

```yaml
scenario: provider-first
# Optional overrides of what the scenario implies:
ahead: writer             # writer | reader | either | neither
documentsLive: moment     # moment | retention | indefinitely
readerPicksSchema: false
schema: prescribes        # prescribes | describes
# Required when documentsLive is retention:
oldestLive: 1.2.0
# Required for each major past the first, when documents live indefinitely and the reader doesn't pick its schema:
migrations:
  2: The Accounts service rewrites each profile as it loads it. See its runbook.
```

Each scenario implies these facts:

| Scenario | `ahead` | `documentsLive` | `readerPicksSchema` | `schema` |
| --- | --- | --- | --- | --- |
| `provider-first` | writer | moment | no | prescribes |
| `consumer-first` | reader | moment | no | prescribes |
| `config-file` | reader | indefinitely | no | prescribes |
| `stored-one-schema` | reader | indefinitely | no | prescribes |
| `stored-self-naming` | neither | indefinitely | yes | prescribes |
| `retained-stream` | either | retention | no | prescribes |
| `uncoordinated` | either | states it | no | prescribes |
| `lock-step` | neither | moment | no | prescribes |
| `third-party-format` | reader | states it | no | describes |

**Two scenarios must state `documentsLive`.** How long documents live varies in `uncoordinated` and `third-party-format`, so the scenario alone is refused, and the family says which it is. A chat app's synced notes may be kept for good, and a webhook's messages may be read and gone. **If you don't know how long your documents live, choose `indefinitely`.** It is always accepted, and it is the safe answer, because it assumes old documents may still be out there.

**A retention family gives its oldest live version.** `retained-stream`, or any family that sets `documentsLive: retention`, must have `oldestLive`. `jsm create-schema` starts it at `1.0.0`. It must be a version the family has, so a mistyped version can't quietly switch the checks off.

**Every command that reads a family needs its declaration.** `validate`, `build-dist`, `check-party`, `check-changes`, `diff` and `create-schema-version` fail on a family with no declaration, or an invalid one, and say how to declare it. Only `create-registry`, `render-schema` and `tag-deployment` don't need one. An existing family with no `family.yml` gets one by hand: there's no migration command.

**A declaration is fixed once a family is deployed.** Where an environment doesn't allow schema mutation, `check-changes` fails on a change to the declaration of a family with a deployed version, meaning one at the newest `jsm-deploy/<env>/*` tag reachable from `HEAD`. A family is one flow, so a changed scenario means a new family. It allows three things: moving `oldestLive` forward, adding or changing a migration statement, and adding a declaration to a family that had none. A whole family may go only if [each of its versions may](#how-long-a-version-lasts), as judged by the declaration it was deployed with.

The other facts bear on the rule like this. A reader that picks its schema from the document means no cross-version runs, so that family behaves as `neither`. `describes` changes the wording of failures, not which documents run. How long documents live decides [how long a version lasts](#how-long-a-version-lasts) and [what a major bump means](#what-a-major-bump-means).

Of the nine, only scenario 5 has a reader that picks its schema from the document, and only scenario 9 describes a format rather than prescribing one.

## The rule

A writer ahead of its reader needs **no loosening**: a later version accepts nothing an earlier one rejects. A reader ahead of its writer needs **no tightening**: a later version rejects nothing an earlier one accepts.

Which documents prove that depends on which versions can meet, within a major:

| Document run | Proves | Writer ahead | Reader ahead | Either | Neither |
| --- | --- | --- | --- | --- | --- |
| Later pass on earlier version | No loosening | Yes | No | Yes | No |
| Earlier fail on later version | No loosening | Yes | No | Yes | No |
| Earlier pass on later version | No tightening | No | Yes | Yes | No |
| Later fail on earlier version | No tightening | No | Yes | Yes | No |
| Own documents on own version | The version behaves as intended | Yes | Yes | Yes | Yes |

Each pair of versions in a major is checked, not only neighbouring versions, so a `1.2.0` document runs on `1.0.0` as well as on `1.1.0`. Versions are those on disk, leaving out any [below `oldestLive`](#how-long-a-version-lasts). The same pairs run however a version is selected, and each runs once.

A failure always appears under the later version of its pair, whichever version you asked for, and the exit code is non-zero if any run fails. It names the rule that broke, both versions and the document, and says what that means for the family's scenario. For a family that describes someone else's format, it says the real format has changed, not that you made a mistake. Text, JSON and JUnit reports all name them.

## How long a version lasts

Deleting a version is safe only if no document still needs it. So what `check-changes` allows, in an environment which doesn't allow schema mutation, follows how long the family's documents live:

| `documentsLive` | Deleting a deployed version | Why |
| --- | --- | --- |
| `moment` | Allowed | Documents are read and gone, so a version nobody uses any more can go. |
| `retention` | Allowed below `oldestLive`, refused at it or above | `oldestLive` is the oldest version whose documents may still exist. |
| `indefinitely` | Refused | A document kept for years would be left with no schema that describes it. |

Deleting a whole family is judged the same way, version by version, by the declaration it was deployed with. So a family whose documents live indefinitely can't be deleted from a deployed environment by accident, or at all.

**`oldestLive` limits the rule's pairs too.** A version below it takes no part: no document runs between it and another version, the [unexercised-change](#unexercised-change-warned) comparison leaves it out, and a later version no longer inherits its documents. So a family can move on from a mistake in `1.0.0` once no `1.0.0` document remains. It's still a version: its own documents still run on it, and `--strict` asks of it what it always did. Across majors the comparison is by version number, so `oldestLive: 2.0.0` retires all of major 1.

**Moving `oldestLive` forward is a change someone reviews.** JSM uses no clock and no deploy tags for this, so the same commit always gives the same result. If nobody moves it, the checks simply stay stricter than they need to be. It can't move back, and it must name a version the family has. A change may move it forward and delete the versions it passes, together.

Moving it forward can ask for two things:

- The new oldest live version inherits nothing from below it, so `--strict` says if it now has no `pass` or `fail` document of its own. A deployed version may gain test documents.
- An entry in a later version's `compat-exceptions.yml` naming a version now below `oldestLive` clears nothing, so it is stale, and fails. `check-changes` lets you remove it, as it does when the version is deleted.

`check-party` warns about a listed version below `oldestLive`, and runs nothing for it, since the rule no longer holds later versions to it. A party still on that version can be harmed unseen, and `--strict` makes that a failure, so move it to a version the family still checks.

## What a major bump means

No document crosses a major, so a major bump is how a family makes a change its rule forbids. What that leaves to you depends on the scenario. In all of them, a migration is your code, not JSM's: JSM doesn't own your data, so it records your statement, and tests no data.

| Scenario | What a major bump means |
| --- | --- |
| 1. Provider moves first | A new flow, run side by side with the old one. Readers move first, then writers stop sending the old major. Nothing needs migrating, since messages are gone in moments. |
| 2. Consumer moves first | A new flow, run side by side with the old one. Writers move, then the reader stops accepting the old major. Nothing needs migrating. |
| 3. Config files | Files written for the old major are still in people's repositories, so the tool, or a tool you ship, must move them. **Needs a migration statement.** |
| 4. Stored data, one schema for all | The old documents are still in the database, so your code must move them before the new major can read them. **Needs a migration statement.** |
| 5. Stored data, each document names its version | Old documents keep naming their old version, and the reader goes on validating them against it. The reader must handle both, and nothing needs migrating. |
| 6. Retained streams | Old events stay in the log until retention ends, so a reader replaying it must handle both majors until then. Once the last old-major event has gone, move `oldestLive` into the new major. |
| 7. Uncoordinated parties | Run both majors side by side for as long as parties remain on the old one, which you may not control. If the family states `documentsLive: indefinitely`, **it needs a migration statement.** |
| 8. Lock-step | Both sides move together, so a major bump means no more than any other version does. |
| 9. A format someone else owns | The format's owner already made the change, and choosing a major changes nothing about the documents out there. If the family states `documentsLive: indefinitely`, **it needs a migration statement** saying what happens to what you've kept. |

**The migration statement.** Where old documents live indefinitely and the reader doesn't pick its schema from the document, each major past the family's first needs a sentence or a link in `family.yml`, under `migrations`, keyed by major:

```yaml
scenario: stored-one-schema
migrations:
  2: The Accounts service rewrites each profile as it loads it. See its runbook.
```

Without one, `validate` fails on the versions of that major, under its first version, and earlier majors are unaffected. JSM checks only that a statement is there. `jsm create-schema-version` says when a major bump needs one. A statement isn't part of what `check-changes` fixes, since a deployed family has to gain one for each new major, and a link can move.

## Deliberate exceptions

Sometimes a change that breaks the rule is right. Take a provider-first family whose `1.0.0` wrongly rejected names over 50 characters, and a `1.1.0` that fixes it: `1.0.0`'s `fail` document with a long name is now accepted, and that is the point. Say so in `compat-exceptions.yml`, in the later version's directory, beside its schema:

```yaml
exceptions:
  - version: 1.0.0
    document: fail/too-long.json
    rule: no-loosening
    reason: 1.0.0 wrongly rejected names over 50 characters.
```

- **`version`** is the earlier version of the pair, in the same major.
- **`document`** is the document, as `pass/<name>.json` or `fail/<name>.json`. It belongs to the earlier version or to the later one, as [the rule](#the-rule) says for that rule and type of document: here, `no-loosening` runs an earlier `fail` document on the later version, so this is `1.0.0`'s.
- **`rule`** is the rule the run breaks, `no-loosening` or `no-tightening`. A version's own documents can't be excepted.
- **`reason`** is required, and says why this break is deliberate.

An entry clears that one run and nothing else: another document, another earlier version or another rule still fails. An excepted run doesn't fail, and the run exits `0`, but the report shows it, with its reason. The text report marks it `excepted`, the JSON report lists it under `excepted` with the status `excepted`, and in JUnit it's a skipped test case.

**A stale exception fails.** An entry that names a version or document that doesn't exist, a rule the family doesn't hold its versions to, or a run that doesn't fail clears nothing, and it fails `validate` and `build-dist`, so entries don't pile up. A file JSM can't read, such as an entry with no reason, stops the command.

**An exception belongs to one version.** `create-schema-version` never copies the file. If `1.2.0` has the same break with `1.0.0`, its own file lists it.

### Acknowledging a warning no document can clear

The same file can acknowledge an [unexercised-change warning](./developing-schemas.md#unexercised-changes) that no document can clear, because the structural comparison sees a change where the documents accepted are the same. Moving `maxLength` into an `allOf` is one: the comparison sees a limit removed, but no value is accepted that wasn't before, so no `pass` document can use it. Moving a `minimum` to an `exclusiveMinimum` is another.

```yaml
exceptions:
  - version: 1.0.0
    change: "/name: maxLength removes the limit of 5"
    reason: The limit moved into allOf. It still applies.
```

- **`change`** is the change as the warning prints it, without its `(widens)`. It takes the place of `document` and `rule`.
- **`version`** and **`reason`** are as above.

An entry clears that one warning. A change a later version carries over unchanged warns only under the version that made it, so that later version needs no entry of its own. An entry is stale, and fails, if no such change between those versions needs acknowledging, whether the change has gone, a document now shows it, or the family doesn't protect that direction. `check-changes` locks it once it's deployed, as any other entry. It may be removed once its earlier version has gone.

### The way out of a red deployed version

A new check can fail on versions already deployed, whose schemas and documents can no longer change. An upgrade of JSM, or a family declaring its scenario for the first time, can do this. You can't fix the schema, and deleting a published version harms its readers. List the break instead:

1. Run `jsm validate all --continue-on-error`, and see which runs fail. Each failure names the rule, both versions and the document.
2. For each, decide whether it really is harmless, or was already out in the world and has to stand. If it's a change you can still make differently, in a version that isn't deployed, do that instead.
3. Add `compat-exceptions.yml` to the later version's directory, with one entry for each run, and a reason for each.

`check-changes` allows this, in an environment which doesn't allow schema mutation: a deployed version may gain the file, and a file may gain entries. It refuses editing or removing an entry that was there at the deployment, so the file is reviewed once and then locked, as the schema is. The one exception is an entry whose earlier version or document has since gone, or whose earlier version has fallen below `oldestLive`, which is stale, so it may be removed.

## The promises

JSM's own tests prove each promise below, on purpose-made schemas in `cmd/jsm/testdata/behaviour-registry`, and a test fails if a promise has nothing proving it. They show JSM does what it says. They can't show how your schemas would fare, since [checks only sample](#checks-only-sample).

- <a id="own-documents"></a>**Own documents.** A version accepts its own `pass` documents and rejects its own `fail` documents.
- <a id="no-loosening"></a>**No loosening.** Where a writer can be ahead, a later version in a major accepts nothing an earlier one rejects, as far as the documents show: later `pass` documents run on earlier versions, and earlier `fail` documents on later versions.
- <a id="no-tightening"></a>**No tightening.** Where a reader can be ahead, a later version in a major rejects nothing an earlier one accepts, as far as the documents show: earlier `pass` documents run on later versions, and later `fail` documents on earlier versions.
- <a id="only-versions-that-meet"></a>**Only versions that can meet are held to each other.** A writer-ahead family may tighten, a reader-ahead family may loosen, and a family with neither ahead, or whose reader picks its schema from the document, runs only each version's own documents.
- <a id="version-inherits-documents"></a>**A version inherits documents, where its family runs them across versions.** `--strict` asks each version to have a `pass` and a `fail` document, in its own directory or an earlier version's in its major. The first version of a major has nothing to inherit, nor does a version from below `oldestLive`, and a family with neither side ahead, or whose reader picks its schema from the document, inherits nothing, so its versions each need their own.
- <a id="unexercised-change-warned"></a>**Unexercised changes are warned about.** Between any two versions in a major, `validate` warns about a change in a direction the family protects which no document shows, and `--strict` makes that warning a failure. A writer-ahead family is warned about widenings, a reader-ahead family about narrowings, an either family about both, and a neither family about none.
- <a id="no-document-crosses-a-major"></a>**No document crosses a major.** A new major may change anything, and its documents never run on another major's versions, or theirs on it.
- <a id="deployed-versions-fixed"></a>**Deployed versions stay fixed.** Where an environment doesn't allow schema mutation, `check-changes` fails if a deployed version's schema or test documents change. A deployed version may gain a test document, or be deleted as a whole if [its documents can't still exist](#version-lifecycle).
- <a id="version-lifecycle"></a>**A version lasts as long as its documents may.** Where an environment doesn't allow schema mutation, `check-changes` refuses deleting a deployed version whose family's documents live indefinitely, or one at or above `oldestLive` in a retention family, and allows it otherwise. A version below `oldestLive` takes no part in the rule's pairs.
- <a id="major-needs-migration"></a>**A new major says how old documents move, where they stay around.** Where documents live indefinitely and the reader doesn't pick its schema from the document, `validate` fails on a major past the first with no migration statement in `family.yml`. JSM checks only that it's there.
- <a id="declared-exception"></a>**An exception clears only what it names.** A version's `compat-exceptions.yml` clears the runs it lists, and each gives a reason. Any other failure still fails, an entry which clears nothing fails, and once deployed, an entry stays as it is, though more can be added.

## What the checks can and cannot show

### Checks only sample

Documents sample behaviour. A check that passes shows that none of your documents exposed a break, not that no break exists.

A fail document can be rejected by a later version for a different reason, which hides a loosening, so each fail document should hold one fault. Take a `1.0.0` `fail` document, `{"status": "refunded", "quantity": "two"}`, which has both a status and a quantity that `1.0.0` rejects. If `1.1.0` allows `"refunded"`, the document is still rejected, because of its quantity, and the loosening goes unseen. Split it into two documents, and the one with only the status catches it.

JSM doesn't generate documents of its own to look for these gaps. Your documents and the [unexercised-change warnings](#unexercised-change-warned) are what cover them.

### "Either" is only as strong as its samples

Adding a typed optional property to an open schema is strictly a tightening, and passes only if no document uses the wrong type there.

Take an open `1.0.0` with no `nickname` property. It accepts `{"nickname": 42}`, since an open schema allows any extra property. If `1.1.0` adds `nickname` with `"type": "string"`, it rejects that document. In an either family this is a real tightening, and the checks pass unless one of your documents happens to have a number there.

## How to choose a scenario

Answer these in order, and stop at the first that fits. If an API operation brought you here, take its request and its response separately, as they are two families.

1. **Does another organisation own the format, so your schema only records it?** Scenario 9, [a format someone else owns](#9-a-format-someone-else-owns).
2. **Does each document name its own version, with the reader validating it against that version?** Scenario 5, [stored data, each document names its version](#5-stored-data-each-document-names-its-version).
3. **Are writer and reader always released together, with no documents kept beyond the release?** Scenario 8, [lock-step](#8-lock-step).
4. **Do documents stay around for good, and are they read by newer code?** If people edit them as settings, scenario 3, [config files](#3-config-files). If they are saved records, scenario 4, [stored data, one schema for all](#4-stored-data-one-schema-for-all).
5. **Are documents kept for a set period, and can they be read again?** Scenario 6, [retained streams](#6-retained-streams).
6. **Otherwise, documents are read and gone, so who upgrades first?**
   - The writer, always: scenario 1, [provider moves first](#1-provider-moves-first).
   - The reader, always: scenario 2, [consumer moves first](#2-consumer-moves-first).
   - Nobody controls it, or you can't tell: scenario 7, [uncoordinated parties](#7-uncoordinated-parties).

## Terms from other schema registries

Writer-ahead is what Confluent Schema Registry calls FORWARD_TRANSITIVE, and reader-ahead is BACKWARD_TRANSITIVE, bounded by the major, and tested by documents rather than by comparing schemas. In the same terms, either is FULL_TRANSITIVE, and neither is NONE.

