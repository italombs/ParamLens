-- Historical reference: the hand-written query that ParamLens replaced (CHARINDEX + STRING_SPLIT).
-- It broke on commas inside strings, on "',@" inside the SQL and on multiple IN lists. Not used by the app.

--identifying rows
--ALTER TABLE [table_xe_events] ADD id INT NOT NULL IDENTITY(1,1)

GO


----------
--preview
----------

--processing 'IN' clauses

;WITH extract_lists AS(

	SELECT 
		SUBSTRING(sql_text, 1,CHARINDEX(')SELECT', sql_text)) AS __params,
		SUBSTRING(sql_text, CHARINDEX('[MESSAGETYPEID] IN (', sql_text), 99999) AS messageTypeIdList,
		SUBSTRING(sql_text, CHARINDEX('[ASSIGNEEID] IN (', sql_text), 99999) AS assignedIdList,
		sql_text,
		[statement]
	FROM __xe_event_file 
	WHERE [name] LIKE 'rpc_completed'
	AND sql_text LIKE '%count%MESSAGEIN%MESSAGETYPE%MESSAGEINREAD%PATIENT%PATIENTADDITIONALINFO%MESSAGEINPROCESSOUTCOME%TASK%WHERE%ORGANIZATIONID%ASSIGNEETYPEID%'

)

SELECT
	*,
	REPLACE(SUBSTRING(messageTypeIdList, 0, CHARINDEX(')',messageTypeIdList)), '[MESSAGETYPEID] IN (', '') AS messageTypeIdFilter,
	REPLACE(SUBSTRING(assignedIdList, 0, CHARINDEX(')',assignedIdList)), '[ASSIGNEEID] IN (', '') AS assignedIdFilter
FROM extract_lists 


--Extracting parameter values line by line
SELECT 
	id,
	--Y.value AS _params, 
	REPLACE(SUBSTRING(Y.value, 0, CHARINDEX('=', [value])), '@', '') AS _key_param, 
	SUBSTRING(Y.value, CHARINDEX('=', [value]) + 1, 999) AS _value_param
FROM(

SELECT 
	id
	,SUBSTRING(
			statement,
			CHARINDEX(''',@', statement) + 2,
			LEN(statement)
		 ) AS params
FROM __xe_event_file
WHERE [name] = 'rpc_completed'
) X

CROSS APPLY string_split(params,',') Y


----------
--process
----------

--processing 'IN' clauses
DROP TABLE IF EXISTS #__tmp_xe_event_file

;WITH extract_lists AS(

	SELECT 
		*,
		SUBSTRING(sql_text, CHARINDEX('[MESSAGETYPEID] IN (', sql_text), 99999) AS messageTypeIdList,
		SUBSTRING(sql_text, CHARINDEX('[ASSIGNEEID] IN (', sql_text), 99999) AS assignedIdList
	FROM __xe_event_file 
	WHERE [name] LIKE 'rpc_completed'
	AND sql_text LIKE '%count%MESSAGEIN%MESSAGETYPE%MESSAGEINREAD%PATIENT%PATIENTADDITIONALINFO%MESSAGEINPROCESSOUTCOME%TASK%WHERE%ORGANIZATIONID%ASSIGNEETYPEID%'

)

SELECT
	*,
	REPLACE(SUBSTRING(messageTypeIdList, 0, CHARINDEX(')',messageTypeIdList)), '[MESSAGETYPEID] IN (', '') AS messageTypeIdFilter,
	REPLACE(SUBSTRING(assignedIdList, 0, CHARINDEX(')',assignedIdList)), '[ASSIGNEEID] IN (', '') AS assignedIdFilter
INTO #__tmp_xe_event_file
FROM extract_lists 


--creating table
CREATE TABLE __params(
	qploEpisodeID nvarchar(4000),
	qploEncounter_PatientId bigint,
	qpinOwnerTypeIdPatient int,
	qpinSubjectTypeIdPartialEnconter int,
	qpinDocumentTypeIdPatientDocument int,
	qpinRDVersion_Clinical int,
	qpboAllEpisodes bit,
	qpinImportance_Regular int,
	qpinImportance_High int,
	qpinStartingIndex int,
	qpinMaxRecords int
)

INSERT INTO __params(
	qploEpisodeID,
	qploEncounter_PatientId,
	qpinOwnerTypeIdPatient,
	qpinSubjectTypeIdPartialEnconter,
	qpinDocumentTypeIdPatientDocument,
	qpinRDVersion_Clinical,
	qpboAllEpisodes,
	qpinImportance_Regular,
	qpinImportance_High,
	qpinStartingIndex,
	qpinMaxRecords
)

SELECT
	messageTypeIdFilter AS __x,
	assignedIdFilter AS __y,
    MAX(CASE WHEN _key_param = 'qpinMedOvd'
             THEN _value_param END) AS qpinMedOvd,

    MAX(CASE WHEN _key_param = 'qploOrganizationUserId'
             THEN _value_param END) AS qploOrganizationUserId,

    MAX(CASE WHEN _key_param = 'qpinAssignment_OutcomeTypeId'
             THEN _value_param END) AS qpinAssignment_OutcomeTypeId,

    MAX(CASE WHEN _key_param = 'qploOrganizationId'
             THEN _value_param END) AS qploOrganizationId,

    MAX(CASE WHEN _key_param = 'qpinMessageStateIdAutomaticProcessing'
             THEN _value_param END) AS qpinMessageStateIdAutomaticProcessing,

    MAX(CASE WHEN _key_param = 'qpdaStartDate'
             THEN _value_param END) AS qpdaStartDate,

    MAX(CASE WHEN _key_param = 'qpdaNullDate'
             THEN _value_param END) AS qpdaNullDate,

    MAX(CASE WHEN _key_param = 'qpdaEndDate'
             THEN _value_param END) AS qpdaEndDate,

    MAX(CASE WHEN _key_param = 'qpboUnreadOnly'
             THEN _value_param END) AS qpboUnreadOnly,

    MAX(CASE WHEN _key_param = 'qpboReadOnly'
             THEN _value_param END) AS qpboReadOnly,

    MAX(CASE WHEN _key_param = 'qpstGPOfRegistration_AGB'
             THEN _value_param END) AS qpstGPOfRegistration_AGB,

    MAX(CASE WHEN _key_param = 'qploDropdownAssigneeId'
             THEN _value_param END) AS qploDropdownAssigneeId,

    MAX(CASE WHEN _key_param = 'qpboIsMyGroup'
             THEN _value_param END) AS qpboIsMyGroup,

    MAX(CASE WHEN _key_param = 'qploDropdownAssigneeTypeId'
             THEN _value_param END) AS qploDropdownAssigneeTypeId,

    MAX(CASE WHEN _key_param = 'qploAssigneeType_UserGroup'
             THEN _value_param END) AS qploAssigneeType_UserGroup

FROM
(
    SELECT
        X.id,
		X.messageTypeIdFilter,
		X.assignedIdFilter,
        REPLACE(
            SUBSTRING(Y.value, 1, CHARINDEX('=', Y.value) - 1),
            '@',
            ''
        ) AS _key_param,
        SUBSTRING(
            Y.value,
            CHARINDEX('=', Y.value) + 1,
            999
        ) AS _value_param
    FROM
    (
        SELECT
            id,
			messageTypeIdFilter,
			assignedIdFilter,
			SUBSTRING(
                statement,
                CHARINDEX(''',@', statement) + 2,
                LEN(statement)
            ) AS params
        FROM #__tmp_xe_event_file
        WHERE [name] = 'rpc_completed'
    ) X
    CROSS APPLY STRING_SPLIT(X.params, ',') Y
) P
GROUP BY id, messageTypeIdFilter, assignedIdFilter;