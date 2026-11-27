package com.contentplatform.app.kyc

data class CreatorVerification(
    val id: String,
    val status: String,
    val legalName: String,
    val countryCode: String,
    val dateOfBirth: String,
    val reasonCode: String,
)

data class KycState(
    val verification: CreatorVerification? = null,
    val loading: Boolean = false,
    val submitting: Boolean = false,
    val error: String? = null,
    val notice: String? = null,
    val queue: List<CreatorVerification> = emptyList(),
)
