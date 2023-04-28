CREATE DATABASE IF NOT EXISTS `iris_message_processor`

--
-- Table structure for table `message`
--

DROP TABLE IF EXISTS `message`;
CREATE TABLE `message` (
  `id` varchar(36) NOT NULL,
  `batch` varchar(36) DEFAULT NULL,
  `sent` datetime DEFAULT NULL,
  `application` varchar(255) NOT NULL,
  `target` varchar(255) NOT NULL,
  `destination` varchar(255) DEFAULT NULL,
  `mode` varchar(255) DEFAULT NULL,
  `priority` varchar(36) DEFAULT "",
  `plan` varchar(255) DEFAULT NULL,
  `subject` varchar(255) DEFAULT NULL,
  `body` text,
  `incident_id` bigint(20) DEFAULT NULL,
  `step` int(11) DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `plan` (`plan`),
  KEY `ix_message_sent` (`sent`),
  KEY `ix_message_incident_id` (`incident_id`),
  KEY `ix_message_batch` (`batch`),
  KEY `ix_message_application` (`application`),
  KEY `ix_message_target` (`target`),
  KEY `ix_message_mode` (`mode`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;


--
-- Table structure for table `message_sent_status`
--

DROP TABLE IF EXISTS `message_sent_status`;
CREATE TABLE `message_sent_status` (
  `message_id` varchar(36) NOT NULL,
  `status` varchar(255) DEFAULT NULL,
  `vendor_message_id` varchar(255) DEFAULT NULL,
  `last_updated` datetime NOT NULL,
  PRIMARY KEY (`message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;


--
-- Table structure for table `message_changelog`
--

DROP TABLE IF EXISTS `message_changelog`;
CREATE TABLE `message_changelog` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `date` datetime NOT NULL,
  `message_id` varchar(36) NOT NULL,
  `change_type` varchar(255) NOT NULL,
  `old` varchar(255) NOT NULL,
  `new` varchar(255) NOT NULL,
  `description` varchar(255),
  PRIMARY KEY (`id`),
  KEY `ix_message_changelog_message_id` (`message_id`),
  KEY `ix_message_changelog_date` (`date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;