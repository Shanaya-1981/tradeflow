# Learner Lab provides a pre-existing LabRole with broad permissions
# We use it for both execution role and task role
data "aws_iam_role" "lab_role" {
  name = "LabRole"
}
