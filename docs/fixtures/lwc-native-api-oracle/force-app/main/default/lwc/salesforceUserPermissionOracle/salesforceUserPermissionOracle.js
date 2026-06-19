import { LightningElement } from "lwc";
import value from "@salesforce/userPermission/ViewSetup";

export default class Oracle extends LightningElement {
  label = "@salesforce/userPermission/";
  permission = value;
}
